"""Explicit test inventory. Discovery detects omissions, but never executes files."""

import argparse
from pathlib import Path
import re

try:
    from .common import ROOT, load_json
except ImportError:
    from common import ROOT, load_json

PROFILES = ("python", "node", "browser", "browser_component")


def discover(root):
    candidates = set()
    for folder in ("tools", "ai/tests"):
        for path in (root / folder).rglob("*"):
            if any(part in {"node_modules", "__pycache__", ".venv"} for part in path.parts):
                continue
            if path.is_file() and (
                re.fullmatch(r"test_.*\.py", path.name)
                or re.search(r"(?:[._]test\.mjs|_smoke\.(?:js|mjs))$", path.name)
                or path.name == "ci_ui_smoke.mjs"
            ):
                candidates.add(path.relative_to(root).as_posix())
    return candidates


def validate(value, root=ROOT):
    if value.get("schema") != 1:
        raise ValueError("Unsupported test registry")
    registered = []
    for profile in PROFILES:
        for entry in value[profile]:
            if not isinstance(entry, str):
                raise ValueError("Test entry must be a relative path")
            registered.append(entry)
    for classification in ("auxiliary", "manual"):
        for entry in value[classification]:
            if not entry.get("reason", "").strip():
                raise ValueError("Non-automatic test needs a reason")
            registered.append(entry["path"])
    if len(set(registered)) != len(registered):
        raise ValueError("Duplicate test registration")
    for entry in registered:
        path = Path(entry)
        if path.is_absolute() or ".." in path.parts or not (root / path).is_file():
            raise ValueError(f"Missing or invalid test entry: {entry}")
    discovered = discover(root)
    if set(registered) != discovered:
        raise ValueError(
            f"Test inventory mismatch: unregistered={sorted(discovered - set(registered))}; "
            f"not-discovered={sorted(set(registered) - discovered)}"
        )
    arguments = value.get("component_arguments", {})
    extra = set(arguments) - set(value["browser_component"])
    if extra:
        raise ValueError("Unregistered component arguments")
    return value


def load(root=ROOT):
    return validate(load_json(root / "tools/ci/registry.json"), root)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", choices=PROFILES)
    options = parser.parse_args()
    registry = load()
    for name in registry[options.profile] if options.profile else sorted(discover(ROOT)):
        print(name)
