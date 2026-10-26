"""Private architecture-provider configuration; never import business secrets."""
import argparse
import getpass
import json
import os
from pathlib import Path
import stat
import urllib.error
import urllib.request

HERE = Path(__file__).resolve().parent
CONFIG = json.loads((HERE / "toolchain.json").read_text())["provider"]
PRIVATE = Path.home() / ".config/artwslbot/architecture"
KEY_FILE = PRIVATE / "provider.key"


def private_dir(path):
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or path.stat().st_mode & 0o077:
        raise ValueError("Private directory must not be a symlink or accessible by other users")


def read_key():
    key = os.environ.get(CONFIG["credential_env"], "")
    if not key:
        metadata = KEY_FILE.lstat()
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_mode & 0o077:
            raise ValueError("Credential must be a regular mode-0600 file")
        if metadata.st_uid != os.getuid():
            raise ValueError("Credential owner mismatch")
        key = KEY_FILE.read_text().strip()
    if not key or "\n" in key or "\r" in key:
        raise ValueError("Missing or malformed architecture credential")
    return key


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request(path, body=None):
    # Fixed HTTPS origin; redirects and ambient proxy credentials are disallowed.
    if CONFIG["base_url"] != "https://www.sevnx.lol/v1":
        raise ValueError("Provider origin change requires explicit code review")
    req = urllib.request.Request(
        CONFIG["base_url"] + path,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + read_key(), "Content-Type": "application/json",
                 "User-Agent": "ARTWSLBOt-Architecture/1.0"},
    )
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(req, timeout=45) as response:
            raw = response.read(2_000_001)
            if len(raw) > 2_000_000:
                raise ValueError("Provider response exceeds limit")
            return json.loads(raw)
    except urllib.error.HTTPError as exc:
        raise ValueError(f"Provider returned HTTP {exc.code}; body suppressed") from None
    except (urllib.error.URLError, TimeoutError):
        raise ValueError("Provider transport failed; details suppressed") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["configure", "preflight"])
    args = parser.parse_args()
    if args.command == "configure":
        private_dir(PRIVATE)
        key = getpass.getpass("Architecture provider API key (hidden): ")
        if not key or any(ch.isspace() for ch in key):
            raise ValueError("Malformed credential")
        fd = os.open(KEY_FILE, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "w") as out:
            os.fchmod(out.fileno(), 0o600)
            out.write(key + "\n")
        print("Credential saved outside the repository with mode 0600.")
        return
    models = request("/models")
    found = any(item.get("id") == CONFIG["model"] for item in models.get("data", []))
    print(json.dumps({"model": CONFIG["model"], "listed": found}))
    result = request("/chat/completions", {
        "model": CONFIG["model"],
        "messages": [{"role": "user", "content": 'Return only JSON: {"ok":true}'}],
        "response_format": {"type": "json_object"},
        "max_tokens": 128,
        "temperature": 0,
        "stream": False,
    })
    content = result.get("choices", [{}])[0].get("message", {}).get("content", "")
    if json.loads(content) != {"ok": True}:
        raise ValueError("Provider JSON-mode preflight failed; content suppressed")
    print(json.dumps({"json_mode": "passed", "model": result.get("model"),
                      "usage": result.get("usage", {}), "business_settings_changed": False}))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, json.JSONDecodeError) as error:
        raise SystemExit(str(error)) from None
