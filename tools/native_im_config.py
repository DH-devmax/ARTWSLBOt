"""Extract the IM identifier from resources decoded from the pinned official APK.

No network or login requests. Values are written only to a new private file.
The decoded value must also occur in the hash-verified APK resource table.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import xml.etree.ElementTree as ET
import zipfile

APK_SHA256 = "8f9a1e4483b0dd210b1c2c6e9549776a103af777b08bc04a06252e2fbd91f96d"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("apk", type=Path)
    parser.add_argument("--resources", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if args.apk.stat().st_size != 73335376 or hashlib.sha256(args.apk.read_bytes()).hexdigest() != APK_SHA256:
        raise ValueError("unrecognized official artifact")
    if args.resources.stat().st_size > 2 * 1024 * 1024:
        raise ValueError("oversized resources")
    resources = ET.parse(args.resources).getroot()
    values = [item.text for item in resources.findall("string") if item.get("name") == "str_online_yunxin_key"]
    if len(values) != 1 or not re.fullmatch(r"[a-f0-9]{32}", values[0] or ""):
        raise ValueError("ambiguous production IM identifier")
    value = values[0]
    with zipfile.ZipFile(args.apk) as archive:
        info = archive.getinfo("resources.arsc")
        if info.file_size > 16 * 1024 * 1024:
            raise ValueError("oversized resource table")
        table = archive.read(info)
    if value.encode() not in table and value.encode("utf-16-le") not in table:
        raise ValueError("decoded value is not in the pinned APK")
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as out:
        json.dump({"app_key": value}, out)
        out.write("\n")
    print(json.dumps({"artifact_sha256": APK_SHA256, "im_configuration_created": True, "network_requests": 0}))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Pinned IM inspection failed; no configuration values were printed.")
