"""Record a preceding successful shell check without copying its output."""

import argparse
from pathlib import Path

try:
    from .common import revision, write_json
except ImportError:
    from common import revision, write_json


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--name", required=True)
    parser.add_argument("--output", required=True, type=Path)
    options = parser.parse_args()
    write_json(options.output, {"schema": 1, "sha": revision(),
                               "check": options.name, "status": "passed"})


if __name__ == "__main__":
    main()
