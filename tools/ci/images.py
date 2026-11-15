"""Bind Docker archives to the tested image identity/source; release never rebuilds."""

import argparse
import base64
import hashlib
import json
from pathlib import Path, PurePosixPath
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
INDEX_TYPES = {
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
}
MANIFEST_TYPES = {
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
}
CONFIG_TYPES = {
    "application/vnd.oci.image.config.v1+json",
    "application/vnd.docker.container.image.v1+json",
}


class ImageContractError(ValueError):
    """A safe, static diagnostic suitable for the CI log."""


def archive_member(handle, name, *, limit=None):
    if (not isinstance(name, str) or PurePosixPath(name).is_absolute()
            or ".." in PurePosixPath(name).parts or PurePosixPath(name).as_posix() != name):
        raise ImageContractError("Invalid archive member path")
    members = [member for member in handle.getmembers() if member.name == name]
    if (len(members) != 1 or not members[0].isfile()
            or (limit is not None and members[0].size > limit)):
        raise ImageContractError("Missing, duplicate or invalid archive member")
    return members[0]


def descriptor_blob(handle, descriptor, *, metadata=True):
    if (not isinstance(descriptor, dict)
            or not isinstance(descriptor.get("mediaType"), str)
            or not isinstance(descriptor.get("digest"), str)
            or not IMAGE_ID.fullmatch(descriptor["digest"])
            or type(descriptor.get("size")) is not int or descriptor["size"] < 0):
        raise ImageContractError("Invalid OCI descriptor")
    name = "blobs/" + descriptor["digest"].replace(":", "/")
    member = archive_member(handle, name, limit=1024 * 1024 if metadata else None)
    if member.size != descriptor["size"]:
        raise ImageContractError("OCI descriptor size mismatch")
    checksum, chunks = hashlib.sha256(), []
    with handle.extractfile(member) as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            checksum.update(block)
            if metadata:
                chunks.append(block)
    if "sha256:" + checksum.hexdigest() != descriptor["digest"]:
        raise ImageContractError("OCI descriptor digest mismatch")
    if "data" in descriptor:
        inline = base64.b64decode(descriptor["data"], validate=True)
        if (len(inline) != member.size
                or "sha256:" + hashlib.sha256(inline).hexdigest() != descriptor["digest"]):
            raise ImageContractError("OCI inline content mismatch")
    if metadata:
        value = json.loads(b"".join(chunks))
        if not isinstance(value, dict):
            raise ImageContractError("Invalid OCI metadata")
        return value


def oci_manifest(handle, descriptor, *, attestation=False):
    if descriptor.get("mediaType") not in MANIFEST_TYPES:
        raise ImageContractError("Unsupported OCI manifest type")
    value = descriptor_blob(handle, descriptor)
    config_types = CONFIG_TYPES | {"application/vnd.oci.empty.v1+json"} if attestation else CONFIG_TYPES
    if (value.get("schemaVersion") != 2
            or value.get("mediaType") != descriptor["mediaType"]
            or not isinstance(value.get("config"), dict)
            or value["config"].get("mediaType") not in config_types or "layers" not in value):
        raise ImageContractError("Invalid OCI image manifest")
    config = descriptor_blob(handle, value["config"])
    rootfs = config.get("rootfs")
    # Docker also exports zero-layer scratch images with null layers/diff_ids.
    if (value["layers"] is None and isinstance(rootfs, dict) and rootfs.get("type") == "layers"
            and "diff_ids" in rootfs and rootfs["diff_ids"] in (None, [])):
        value["layers"] = []
    if not isinstance(value["layers"], list):
        raise ImageContractError("Invalid OCI image layers")
    for layer in value["layers"]:
        descriptor_blob(handle, layer, metadata=False)
    return value


def validate_oci_identity(handle, value, docker_manifest, config_id):
    """Containerd IDs identify a manifest/index, not the configuration blob."""
    index = json.load(handle.extractfile(archive_member(handle, "index.json", limit=1024 * 1024)))
    if (not isinstance(index, dict) or index.get("schemaVersion") != 2
            or index.get("mediaType") not in INDEX_TYPES
            or not isinstance(index.get("manifests"), list) or len(index["manifests"]) != 1):
        raise ImageContractError("Invalid OCI archive index")
    target = index["manifests"][0]
    if (not isinstance(target, dict) or target.get("digest") != value["image_id"]
            or not isinstance(target.get("annotations"), dict)
            or target["annotations"].get("io.containerd.image.name")
            != "docker.io/library/" + value["tag"]
            or target["annotations"].get("org.opencontainers.image.ref.name") != "ci"):
        raise ImageContractError("OCI archive does not identify the inspected image")
    if target.get("mediaType") in INDEX_TYPES:
        image_index = descriptor_blob(handle, target)
        entries = image_index.get("manifests")
        if (image_index.get("schemaVersion") != 2
                or image_index.get("mediaType") != target["mediaType"]
                or not isinstance(entries, list) or not entries
                or any(not isinstance(entry, dict)
                       or not isinstance(entry.get("platform"), dict) for entry in entries)):
            raise ImageContractError("Invalid OCI image index")
        images = [entry for entry in entries
                  if entry.get("platform", {}).get("os") == "linux"
                  and entry.get("platform", {}).get("architecture") == "amd64"]
        if len(images) != 1:
            raise ImageContractError("OCI index requires exactly one linux/amd64 image")
        target = images[0]
        for entry in entries:
            if entry is target:
                continue
            annotations = entry.get("annotations", {})
            if (entry.get("platform") != {"os": "unknown", "architecture": "unknown"}
                    or not isinstance(annotations, dict)
                    or annotations.get("vnd.docker.reference.type") != "attestation-manifest"
                    or annotations.get("vnd.docker.reference.digest") != target.get("digest")):
                raise ImageContractError("Unexpected image in OCI index")
            attestation = oci_manifest(handle, entry, attestation=True)
            if ("subject" in attestation and attestation["subject"] != {
                    key: target[key] for key in ("mediaType", "digest", "size")}):
                raise ImageContractError("Attestation subject differs from the tested image")
    manifest = oci_manifest(handle, target)
    if (manifest["config"]["digest"] != config_id
            or docker_manifest["Config"] != "blobs/" + config_id.replace(":", "/")
            or (docker_manifest.get("Layers") or []) != [
                "blobs/" + layer["digest"].replace(":", "/") for layer in manifest["layers"]]):
        raise ImageContractError("Docker and OCI manifests identify different content")


def inspect_image(tag):
    completed = run(["docker", "image", "inspect", "--format",
                     "{{json .Id}} {{json .Config.Labels}}", tag])
    if completed.returncode:
        raise ImageContractError("Image inspection failed")
    identifier, raw = completed.stdout.strip().split(" ", 1)
    image_id, labels = json.loads(identifier), json.loads(raw)
    if not isinstance(image_id, str) or not isinstance(labels, dict):
        raise ImageContractError("Invalid inspected image metadata")
    return image_id, labels


def pack(component, output, sha):
    output.mkdir(parents=True, exist_ok=True)
    image_id, labels = inspect_image(TAGS[component])
    if not IMAGE_ID.fullmatch(image_id) or labels.get("org.opencontainers.image.revision") != sha:
        raise ImageContractError("Image does not match the tested revision")
    archive = output / f"{component}.tar"
    completed = run(["docker", "image", "save", "-o", str(archive), TAGS[component]],
                    timeout=600)
    if completed.returncode:
        raise ImageContractError("Image export failed")
    value = {
        "schema": 1, "sha": sha, "component": component, "tag": TAGS[component],
        "image_id": image_id, "archive": archive.name, "archive_sha256": digest(archive),
        "os": "linux", "architecture": "amd64",
    }
    validate_archive(value, output, sha)
    write_json(output / f"{component}.json", value)
    return value


def validate_archive(value, folder, sha):
    if not isinstance(value, dict):
        raise ImageContractError("Invalid image metadata")
    component = value.get("component")
    if (value.get("schema") != 1 or not isinstance(component, str)
            or component not in COMPONENTS or value.get("sha") != sha):
        raise ImageContractError("Wrong artifact revision or component")
    if (value.get("tag") != TAGS[component] or value.get("archive") != f"{component}.tar"
            or not isinstance(value.get("image_id"), str)
            or not IMAGE_ID.fullmatch(value["image_id"])
            or value.get("os") != "linux" or value.get("architecture") != "amd64"):
        raise ImageContractError("Invalid image metadata")
    archive = folder / value["archive"]
    if archive.is_symlink() or digest(archive) != value.get("archive_sha256"):
        raise ImageContractError("Image archive integrity failure")
    with tarfile.open(archive, mode="r:") as handle:
        member = archive_member(handle, "manifest.json", limit=1024 * 1024)
        manifest = json.load(handle.extractfile(member))
        if (not isinstance(manifest, list) or len(manifest) != 1
                or not isinstance(manifest[0], dict)
                or manifest[0].get("RepoTags") != [TAGS[component]]):
            raise ImageContractError("Unexpected archived image")
        config_path = manifest[0]["Config"]
        config_member = archive_member(handle, config_path, limit=1024 * 1024)
        raw = handle.extractfile(config_member).read()
        config_id = f"sha256:{hashlib.sha256(raw).hexdigest()}"
        if config_id != value["image_id"]:
            validate_oci_identity(handle, value, manifest[0], config_id)
        config = json.loads(raw)
        settings = config.get("config") if isinstance(config, dict) else None
        labels = settings.get("Labels") if isinstance(settings, dict) else None
        if (not isinstance(config, dict)
                or config.get("os") != "linux" or config.get("architecture") != "amd64"
                or not isinstance(labels, dict)
                or labels.get("org.opencontainers.image.revision") != sha):
            raise ImageContractError("Unverified image provenance or platform")
    return value


def load_image(value, folder, sha):
    validate_archive(value, folder, sha)
    completed = run(["docker", "image", "load", "-i", str(folder / value["archive"])],
                    timeout=600)
    if completed.returncode or inspect_image(value["tag"])[0] != value["image_id"]:
        raise ImageContractError("Loaded image differs from tested image identity")


def publish(value, folder, sha, push):
    load_image(value, folder, sha)
    destination = f"ghcr.io/dh-devmax/artwslbot-{value['component']}:sha-{sha}"
    if run(["docker", "tag", value["image_id"], destination]).returncode:
        raise ImageContractError("Cannot tag verified image")
    result = {**value, "destination": destination, "pushed": False}
    if push:
        completed = run(["docker", "push", destination], timeout=1200)
        if completed.returncode:
            raise ImageContractError("Registry publish failed")
        matches = re.findall(r"digest: (sha256:[a-f0-9]{64})",
                             (completed.stdout or "") + (completed.stderr or ""))
        if len(set(matches)) != 1:
            raise ImageContractError("Registry did not return an immutable digest")
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
        raise ImageContractError("Refusing an artifact from a different checkout")
    folder, component = options.directory, options.component
    if options.command == "pack":
        pack(component, folder, sha)
    else:
        value = load_json(folder / f"{component}.json")
        if not isinstance(value, dict) or value.get("component") != component:
            raise ImageContractError("Requested component does not match the archive")
        if options.command == "verify":
            validate_archive(value, folder, sha)
        elif options.command == "load":
            load_image(value, folder, sha)
        else:
            result = publish(value, folder, sha, options.push)
            write_json(folder / f"{component}-release.json", result)
            print(json.dumps(result, sort_keys=True))
    return 0


def cli():
    try:
        return main()
    except ImageContractError as error:
        reason = str(error)
    except (ValueError, KeyError, OSError, TypeError, tarfile.TarError):
        reason = "Malformed or unreadable image artifact"
    print(f"Verified image contract failed: {reason}; no rebuild or retry was attempted.",
          file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(cli())
