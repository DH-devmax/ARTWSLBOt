"""Verify the aggregate gate binds the archives about to be published."""

import argparse
from pathlib import Path
import re
import sys

try:
    from .common import digest, load_json, revision
    from .evidence import JOBS, verify
    from .images import validate_archive
except ImportError:
    from common import digest, load_json, revision
    from evidence import JOBS, verify
    from images import validate_archive


def validate(folder, sha):
    attestation = load_json(folder / "foundation.json")
    receipt_path = folder / "receipt/job-report.json"
    receipts = attestation.get("receipts", {})
    if (attestation.get("schema") != 1 or attestation.get("sha") != sha
            or attestation.get("status") != "passed"
            or set(attestation.get("jobs", [])) != set(JOBS)
            or len(attestation.get("jobs", [])) != len(JOBS)
            or not isinstance(receipts, dict) or set(receipts) != set(JOBS)
            or not all(isinstance(value, str) and re.fullmatch(r"[a-f0-9]{64}", value)
                       for value in receipts.values())
            or receipts.get("images") != digest(receipt_path)):
        raise ValueError("Release requires the exact successful foundation evidence")
    receipt = load_json(receipt_path)
    if verify(receipt, receipt_path.parent, sha) != "images":
        raise ValueError("Wrong build receipt")
    for component in ("api", "web"):
        metadata = folder / "images" / f"{component}.json"
        if digest(metadata) != receipt["files"].get(metadata.name):
            raise ValueError("Image archive belongs to a different verification")
        value = load_json(metadata)
        if value.get("component") != component:
            raise ValueError("Wrong release component")
        validate_archive(value, metadata.parent, sha)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--directory", type=Path, required=True)
    options = parser.parse_args()
    try:
        validate(options.directory, revision())
    except (ValueError, KeyError, OSError):
        print("Release provenance failed; nothing was rebuilt or published.", file=sys.stderr)
        sys.exit(1)
    print("Exact verified API/Web archives are eligible for the protected release.")
