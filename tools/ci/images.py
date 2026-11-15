"""Bind Docker archives to the exact tested source/config; release never rebuilds."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys
import tarfile

try:
    from .common import digest, load_json, revision, run, write_json
except ImportError:
    from common import digest, load_json, revision, run, write_json

COMPONENTS = {"api", "web"}
IMAGE_ID = re.compile(r"sha256:[a-f0-9]{64}")
TAGS = {name: f"wsl-{name}:ci" for name in COMPONENTS}


def inspect_image(tag):
    completed = run(["docker", "image", "inspect", "--format",
                     "{{json .Id}} {{json .Config.Labels}}", tag])
    if completed.returncode:
        raise ValueError("Image inspection failed")
    identifier, raw = completed.stdout.strip().split(" ", 1)
    return json.loads(identifier), json.loads(raw)


def pack(component, output, sha):
    output.mkdir(parents=True, exist_ok=True)
    image_id, labels = inspect_image(TAGS[component])
    if not IMAGE_ID.fullmatch(image_id) or labels.get("org.opencontainers.image.revision") != sha:
        raise ValueError("Image does not match the tested revision")
    archive = output / f"{component}.tar"
    completed = run(["docker", "image", "save", "-o", str(archive), TAGS[component]],
                    timeout=600)
    if completed.returncode:
        raise ValueError("Image export failed")
    value = {
        "schema": 1, "sha": sha, "component": component, "tag": TAGS[component],
        "image_id": image_id, "archive": archive.name, "archive_sha256": digest(archive),
        "os": "linux", "architecture": "amd64",
    }
    validate_archive(value, output, sha)
    write_json(output / f"{component}.json", value)
    return value


def validate_archive(value, folder, sha):
    component = value.get("component")
    if value.get("schema") != 1 or component not in COMPONENTS or value.get("sha") != sha:
        raise ValueError("Wrong artifact revision or component")
    if (value.get("tag") != TAGS[component] or value.get("archive") != f"{component}.tar"
            or not IMAGE_ID.fullmatch(value.get("image_id", ""))):
        raise ValueError("Invalid image metadata")
    archive = folder / value["archive"]
    if archive.is_symlink() or digest(archive) != value.get("archive_sha256"):
        raise ValueError("Image archive integrity failure")
    with tarfile.open(archive, mode="r:") as handle:
        member = handle.getmember("manifest.json")
        if not member.isfile() or member.size > 1024 * 1024:
            raise ValueError("Invalid Docker manifest")
        manifest = json.load(handle.extractfile(member))
        if len(manifest) != 1 or manifest[0].get("RepoTags") != [TAGS[component]]:
            raise ValueError("Unexpected archived image")
        config_path = manifest[0]["Config"]
        if Path(config_path).is_absolute() or ".." in Path(config_path).parts:
            raise ValueError("Invalid configuration path")
        config_member = handle.getmember(config_path)
        if not config_member.isfile() or config_member.size > 1024 * 1024:
            raise ValueError("Invalid Docker configuration")
        raw = handle.extractfile(config_member).read()
        if f"sha256:{hashlib.sha256(raw).hexdigest()}" != value["image_id"]:
            raise ValueError("Configuration digest mismatch")
        config = json.loads(raw)
        if (config.get("os") != "linux" or config.get("architecture") != "amd64"
                or config.get("config", {}).get("Labels", {}).get(
                    "org.opencontainers.image.revision") != sha):
            raise ValueError("Unverified image provenance or platform")
    return value


def load_image(value, folder, sha):
    validate_archive(value, folder, sha)
    completed = run(["docker", "image", "load", "-i", str(folder / value["archive"])],
                    timeout=600)
    if completed.returncode or inspect_image(value["tag"])[0] != value["image_id"]:
        raise ValueError("Loaded image differs from tested configuration")


def publish(value, folder, sha, push):
    load_image(value, folder, sha)
    destination = f"ghcr.io/dh-devmax/artwslbot-{value['component']}:sha-{sha}"
    if run(["docker", "tag", value["image_id"], destination]).returncode:
        raise ValueError("Cannot tag verified image")
    result = {**value, "destination": destination, "pushed": False}
    if push:
        completed = run(["docker", "push", destination], timeout=1200)
        if completed.returncode:
            raise ValueError("Registry publish failed")
        matches = re.findall(r"digest: (sha256:[a-f0-9]{64})",
                             (completed.stdout or "") + (completed.stderr or ""))
        if len(set(matches)) != 1:
            raise ValueError("Registry did not return an immutable digest")
        result.update(pushed=True, registry_digest=matches[0])
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("pack", "verify", "load", "publish"))
    parser.add_argument("--component", choices=sorted(COMPONENTS), required=True)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--sha")
    parser.add_argument("--push", action="store_true")
    options = parser.parse_args()
    sha = options.sha or revision()
    if not re.fullmatch(r"[a-f0-9]{40}", sha) or sha != revision():
        raise ValueError("Refusing an artifact from a different checkout")
    folder, component = options.directory, options.component
    if options.command == "pack":
        pack(component, folder, sha)
    else:
        value = load_json(folder / f"{component}.json")
        if value.get("component") != component:
            raise ValueError("Requested component does not match the archive")
        if options.command == "verify":
            validate_archive(value, folder, sha)
        elif options.command == "load":
            load_image(value, folder, sha)
        else:
            result = publish(value, folder, sha, options.push)
            write_json(folder / f"{component}-release.json", result)
            print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, KeyError, OSError, tarfile.TarError):
        print("Verified image contract failed; no rebuild or retry was attempted.", file=sys.stderr)
        sys.exit(1)
