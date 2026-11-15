"""Keep the protected `foundation` check fail-closed across every required job."""

import argparse
import json
import os
from pathlib import Path
import sys

try:
    from .common import digest, load_json, revision, write_json
    from .evidence import JOBS, verify
except ImportError:
    from common import digest, load_json, revision, write_json
    from evidence import JOBS, verify


def validate(needs, root, sha):
    if set(needs) != set(JOBS):
        raise ValueError("Missing or extra required job")
    if any(value.get("result") != "success" for value in needs.values()):
        raise ValueError("A required job failed, was cancelled or was skipped")
    receipts = list(root.rglob("job-report.json"))
    found = []
    hashes = {}
    for path in receipts:
        job = verify(load_json(path), path.parent, sha)
        found.append(job)
        hashes[job] = digest(path)
    if len(found) != len(JOBS) or set(found) != set(JOBS):
        raise ValueError("Missing or duplicate required job evidence")
    return {"schema": 1, "sha": sha, "status": "passed", "jobs": sorted(found),
            "receipts": hashes}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    options = parser.parse_args()
    result = validate(json.loads(os.environ["CI_NEEDS"]), options.directory, revision())
    write_json(options.output, result)
    print("foundation: all required jobs and revision-bound artifacts passed")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, KeyError, OSError):
        print("foundation failed: missing, stale, cancelled or failed verification.",
              file=sys.stderr)
        sys.exit(1)
