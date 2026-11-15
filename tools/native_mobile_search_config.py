"""Generate a private mobile-search profile from the pinned official 3.7.4 APK.

Offline only: no network, login, client execution, or IM configuration. This
explicit companion leaves the historical APK inspector unchanged.
"""

import argparse
import base64
import json
import os
from pathlib import Path
import re
import secrets
from urllib.parse import urlsplit
import uuid

import native_client_config


MAX_ORIGIN_LENGTH = 2048
FAILURE_MESSAGE = (
    "Pinned mobile-search configuration failed; no configuration values were printed."
)


def deployment(client, origin=None):
    origins = client["origins"]
    if not isinstance(origins, list) or not 1 <= len(origins) <= 16:
        raise ValueError("invalid signed origin list")
    selected = origins[0] if origin is None else origin
    if (not isinstance(selected, str) or not 0 < len(selected) <= MAX_ORIGIN_LENGTH
            or "\\" in selected
            or any(ord(char) < 33 or ord(char) > 126 for char in selected)):
        raise ValueError("invalid signed origin")
    parsed = urlsplit(selected)
    if (parsed.scheme != "https" or not parsed.hostname or len(parsed.hostname) > 253
            or parsed.username is not None or parsed.password is not None
            or parsed.path or parsed.query or parsed.fragment
            or selected != "https://" + parsed.netloc
            or (parsed.port is not None and not 1 <= parsed.port <= 65535)):
        raise ValueError("invalid signed origin")
    if selected not in origins:
        raise ValueError("origin is not in the signed production bootstrap")
    for name in ("body_key", "server_key"):
        if not isinstance(client[name], bytes) or len(client[name]) != 32:
            raise ValueError("invalid pinned key")
    app_hash = client["app_hash"]
    if (not isinstance(app_hash, str) or len(app_hash) != 160
            or re.fullmatch(r"[0-9A-F]{2}(?::[0-9A-F]{2}){31}=[0-9A-F]{64}", app_hash) is None):
        raise ValueError("invalid pinned application identity")
    encoded = lambda value: base64.b64encode(value).decode("ascii")
    return {
        "origin": selected,
        "headers": {"accept": "application/json", "content-type": "application/json"},
        # a1b2aad's mobile generator uses fourteen initially empty SDK fields.
        "metadata": [0] * 14,
        "signing_seed": encoded(secrets.token_bytes(32)),
        "body_key": encoded(client["body_key"]),
        "server_key": encoded(client["server_key"]),
        "captcha_id": native_client_config.CAPTCHA_ID,
        "app_key": "",
        "device_id": str(uuid.uuid4()),
        "protocol_version": "3.7.4",
        "app_version": 337,
        "app_hash": app_hash,
    }


def private_output(path, value):
    # Validate/serialize before creating anything, including on malformed input.
    data = (json.dumps(value, indent=2, allow_nan=False) + "\n").encode("ascii")
    path = Path(path)
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags, 0o600)
    identity = None
    try:
        identity = os.fstat(descriptor)
        os.fchmod(descriptor, 0o600)
        remaining = memoryview(data)
        while remaining:
            written = os.write(descriptor, remaining)
            if written == 0:
                raise OSError("private configuration write failed")
            remaining = remaining[written:]
        os.fsync(descriptor)
    except BaseException:
        try:
            current = path.lstat()
            if identity is not None and (
                current.st_dev, current.st_ino
            ) == (identity.st_dev, identity.st_ino):
                path.unlink()
        except FileNotFoundError:
            pass
        raise
    finally:
        os.close(descriptor)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("apk", type=Path)
    parser.add_argument("--output", required=True, type=Path,
                        help="new private JSON file; existing files and symlinks are rejected")
    parser.add_argument("--origin", help="select one of the signed business origins")
    args = parser.parse_args(argv)
    try:
        client = native_client_config.read_client(args.apk)
        profile = deployment(client, args.origin)
        private_output(args.output, profile)
    except Exception:
        raise SystemExit(FAILURE_MESSAGE) from None
    print(json.dumps({
        "protocol_version": "3.7.4",
        "signed_bootstrap_verified": True,
        "private_configuration_written": True,
        "network_requests": 0,
    }))


if __name__ == "__main__":
    main()
