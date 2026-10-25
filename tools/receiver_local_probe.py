"""Manually operate receive-only mode on the local Docker console.

Defaults to read-only observations. Explicit flags import the separate IM
profile or toggle receiving. Never logs in to native services or sends messages.
"""
import argparse
import http.cookiejar
import json
from pathlib import Path
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--account")
    parser.add_argument("--import-im", type=Path)
    choice = parser.add_mutually_exclusive_group()
    choice.add_argument("--enable-receive", action="store_true")
    choice.add_argument("--disable-receive", action="store_true")
    parser.add_argument("--wait", type=int, default=0)
    args = parser.parse_args()
    if not 0 <= args.wait <= 60:
        raise ValueError("wait out of bounds")
    root = Path(__file__).resolve().parents[1]
    env = dict(line.split("=", 1) for line in (root / ".env").read_text().splitlines()
               if "=" in line and not line.startswith("#"))
    jar = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    origin = "http://127.0.0.1:5173"

    def call(path, method="GET", data=None):
        headers = {"Origin": origin, "Content-Type": "application/json", "X-Workspace": "wsl"}
        for cookie in jar:
            if cookie.name == "wsl_csrf":
                headers["X-CSRF-Token"] = cookie.value
        request = urllib.request.Request(origin + "/api/v1" + path, method=method, headers=headers,
                                         data=json.dumps(data).encode() if data is not None else None)
        with client.open(request, timeout=20) as response:
            return json.load(response)["data"]

    call("/auth/login", "POST", {"username": env["BOOTSTRAP_USERNAME"], "password": env["BOOTSTRAP_PASSWORD"]})
    if args.import_im:
        if args.import_im.stat().st_size > 4096:
            raise ValueError("oversized IM profile")
        profile = json.loads(args.import_im.read_text())
        status = call("/native-im-profile")
        call("/native-im-profile", "PUT", {"version": status["version"], "app_key": profile["app_key"]})
    accounts = [a for a in call("/accounts") if
                (a["id"] == args.account if args.account else a["auth_state"] == "authenticated")]
    if len(accounts) != 1:
        raise ValueError("select exactly one authenticated panel")
    path = "/accounts/" + accounts[0]["id"] + "/receiver"
    status = call(path)
    if args.enable_receive or args.disable_receive:
        call(path, "PUT", {"enabled": args.enable_receive, "revision": status["revision"]})
    deadline = time.monotonic() + args.wait
    while True:
        status = call(path)
        snapshot = status.get("snapshot") or {}
        if time.monotonic() >= deadline:
            break
        time.sleep(1)
    print(json.dumps({
        "enabled": status["enabled"], "observed": status["observed"],
        "state": snapshot.get("state"), "error": snapshot.get("error"),
        "heartbeat_observed": bool(snapshot.get("heartbeat_at")),
        "frames": snapshot.get("frames"), "messages": snapshot.get("messages"),
        "acks": snapshot.get("acks"), "quarantined": snapshot.get("quarantined"),
        "queue_depth": snapshot.get("queue_depth"),
        "durable_p95_ms": snapshot.get("durable_p95_ms"),
        "ack_p95_ms": snapshot.get("ack_p95_ms"),
    }, ensure_ascii=False))
    call("/auth/logout", "POST", {})


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as error:
        raise SystemExit(f"Local receiver operation rejected (HTTP {error.code}); no credentials were printed.")
    except Exception:
        raise SystemExit("Local receiver observation failed; no credentials were printed.")
