"""Print a public request vector using the pinned, unmodified upstream Python codec."""

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

BASELINE = "a672ef93cb7dbfb854ae01ff131267b9e8a1e050"

root = Path(sys.argv[1]).resolve()
commit = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
if commit != BASELINE:
    raise SystemExit("refusing an unpinned protocol oracle")
path = root / "astrbot/core/platform/sources/wangshangliao/wire.py"
spec = importlib.util.spec_from_file_location("upstream_wire", path)
wire = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wire)
plain = b'{"synthetic":true}'
print(json.dumps({
    "commit": commit,
    "plain_hex": plain.hex(),
    "key_hex": bytes(range(32)).hex(),
    "request_id": 42,
    "device": 99,
    "body_hex": wire.seal_business(bytes(range(32)), 42, plain, 99).hex(),
}, indent=2))
