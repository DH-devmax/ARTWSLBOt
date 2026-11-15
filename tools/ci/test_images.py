import hashlib
import io
import json
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

from tools.ci import images
from tools.ci.common import digest, load_json, write_json


OCI_MANIFEST = "application/vnd.oci.image.manifest.v1+json"
OCI_INDEX = "application/vnd.oci.image.index.v1+json"
OCI_CONFIG = "application/vnd.oci.image.config.v1+json"


def write_archive(path, contents):
    with tarfile.open(path, "w") as handle:
        for name, body in contents.items():
            member = tarfile.TarInfo(name)
            member.size = len(body)
            handle.addfile(member, io.BytesIO(body))


def image_fixture(folder, component, sha, *, tag=None, config=None, manifest=None,
                  layout="legacy", attestation_config="empty", layerless=False):
    folder.mkdir(parents=True, exist_ok=True)
    layer = b"\0" * 1024
    if config is None:
        config = {
            "os": "linux", "architecture": "amd64",
            "config": {"Labels": {
                "org.opencontainers.image.revision": sha,
                "org.opencontainers.image.title": component,
            }},
            "rootfs": {"type": "layers", "diff_ids": None if layerless else (
                [] if layout == "legacy" else ["sha256:" + hashlib.sha256(layer).hexdigest()])},
        }
    raw = json.dumps(config).encode()
    identifier = hashlib.sha256(raw).hexdigest()
    archive = folder / f"{component}.tar"
    config_path, layers = identifier + ".json", []
    contents = {config_path: raw}
    image_id = "sha256:" + identifier
    if layout != "legacy":
        contents = {}

        def blob(body, media_type):
            checksum = hashlib.sha256(body).hexdigest()
            contents["blobs/sha256/" + checksum] = body
            return {"mediaType": media_type, "digest": "sha256:" + checksum, "size": len(body)}

        def metadata(body, media_type):
            return blob(json.dumps(body).encode(), media_type)

        config_descriptor = blob(raw, OCI_CONFIG)
        config_path = "blobs/sha256/" + identifier
        layer_descriptors = None if layerless else [
            blob(layer, "application/vnd.oci.image.layer.v1.tar")]
        layers = None if layerless else [
            "blobs/" + descriptor["digest"].replace(":", "/") for descriptor in layer_descriptors]
        target = metadata({
            "schemaVersion": 2, "mediaType": OCI_MANIFEST,
            "config": config_descriptor, "layers": layer_descriptors,
        }, OCI_MANIFEST)
        if layout == "index":
            if attestation_config == "empty":
                attestation = blob(b"{}", "application/vnd.oci.empty.v1+json")
                attestation["data"] = "e30="
            else:
                attestation = metadata({
                    "os": "unknown", "architecture": "unknown",
                    "rootfs": {"type": "layers", "diff_ids": []},
                }, OCI_CONFIG)
            proof = metadata({"predicate": {"synthetic": True}}, "application/vnd.in-toto+json")
            body = {"schemaVersion": 2, "mediaType": OCI_MANIFEST,
                    "config": attestation, "layers": [proof]}
            if attestation_config == "empty":
                body.update(artifactType="application/vnd.docker.attestation.manifest.v1+json",
                            subject=dict(target))
            proof_manifest = metadata(body, OCI_MANIFEST)
            proof_manifest.update(
                platform={"os": "unknown", "architecture": "unknown"},
                annotations={"vnd.docker.reference.type": "attestation-manifest",
                             "vnd.docker.reference.digest": target["digest"]})
            target = metadata({
                "schemaVersion": 2, "mediaType": OCI_INDEX,
                "manifests": [{**target, "platform": {"os": "linux", "architecture": "amd64"}},
                              proof_manifest],
            }, OCI_INDEX)
        image_id = target["digest"]
        target["annotations"] = {
            "io.containerd.image.name": "docker.io/library/" + images.TAGS[component],
            "org.opencontainers.image.ref.name": "ci",
        }
        contents["index.json"] = json.dumps({
            "schemaVersion": 2, "mediaType": OCI_INDEX, "manifests": [target],
        }).encode()
        contents["oci-layout"] = b'{"imageLayoutVersion":"1.0.0"}'
    if manifest is None:
        manifest = [{
            "Config": config_path,
            "RepoTags": [tag if tag is not None else images.TAGS[component]],
            "Layers": layers,
        }]
    contents["manifest.json"] = json.dumps(manifest).encode()
    write_archive(archive, contents)
    value = {
        "schema": 1, "sha": sha, "component": component,
        "tag": images.TAGS[component], "archive": archive.name,
        "archive_sha256": digest(archive), "image_id": image_id,
        "os": "linux", "architecture": "amd64",
    }
    write_json(folder / f"{component}.json", value)
    return value


class ImagesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.folder = Path(self.temporary.name)
        self.sha = "a" * 40
        self.config = {"os": "linux", "architecture": "amd64", "config": {
            "Labels": {"org.opencontainers.image.revision": self.sha}}}

    def fixture(self, *, component="api", **options):
        return image_fixture(self.folder, component, self.sha, **options)

    def rewrite(self, value, change):
        path = self.folder / value["archive"]
        with tarfile.open(path, "r:") as handle:
            contents = {member.name: handle.extractfile(member).read()
                        for member in handle if member.isfile()}
        change(contents)
        write_archive(path, contents)
        value["archive_sha256"] = digest(path)

    def rewrite_index(self, value, change):
        def update(contents):
            index = json.loads(contents["index.json"])
            target = index["manifests"][0]
            body = json.loads(contents["blobs/" + target["digest"].replace(":", "/")])
            change(body)
            raw = json.dumps(body).encode()
            value["image_id"] = "sha256:" + hashlib.sha256(raw).hexdigest()
            target.update(digest=value["image_id"], size=len(raw))
            contents["blobs/" + value["image_id"].replace(":", "/")] = raw
            contents["index.json"] = json.dumps(index).encode()
        self.rewrite(value, update)

    def test_verified_exact_archive(self):
        for component in ("api", "web"):
            for layout in ("legacy", "manifest", "index"):
                with self.subTest(component=component, layout=layout):
                    value = self.fixture(component=component, layout=layout)
                    self.assertIs(value, images.validate_archive(value, self.folder, self.sha))

    def test_old_buildkit_attestation_configuration_is_supported(self):
        value = self.fixture(layout="index", attestation_config="image")
        self.assertIs(value, images.validate_archive(value, self.folder, self.sha))

    def test_null_layers_are_only_accepted_for_a_layerless_configuration(self):
        for layout in ("manifest", "index"):
            with self.subTest(layout=layout):
                value = self.fixture(layout=layout, layerless=True)
                self.assertIs(value, images.validate_archive(value, self.folder, self.sha))
                value = self.fixture(layout=layout, layerless=True, config={
                    **self.config, "rootfs": {"type": "layers", "diff_ids": ["sha256:" + "0" * 64]},
                })
                with self.assertRaisesRegex(images.ImageContractError, "Invalid OCI image layers"):
                    images.validate_archive(value, self.folder, self.sha)

    def test_containerd_pack_load_and_dry_publish_preserve_the_inspected_id(self):
        for component in ("api", "web"):
            for layout in ("manifest", "index"):
                with self.subTest(component=component, layout=layout):
                    value = self.fixture(component=component, layout=layout)
                    output = self.folder / "packed"

                    def docker(command, **options):
                        stdout = ""
                        if command[:3] == ["docker", "image", "inspect"]:
                            stdout = json.dumps(value["image_id"]) + " " + json.dumps({
                                "org.opencontainers.image.revision": self.sha})
                        elif command == ["docker", "image", "save", "-o",
                                         str(output / value["archive"]), images.TAGS[component]]:
                            shutil.copyfile(self.folder / value["archive"],
                                            output / value["archive"])
                        elif command not in (
                                ["docker", "image", "load", "-i", str(output / value["archive"])],
                                ["docker", "tag", value["image_id"],
                                 f"ghcr.io/dh-devmax/artwslbot-{component}:sha-{self.sha}"]):
                            self.fail(f"Unexpected Docker command: {command}")
                        return subprocess.CompletedProcess(command, 0, stdout, "")

                    with patch.object(images, "run", side_effect=docker) as commands:
                        self.assertEqual(value, images.pack(component, output, self.sha))
                        result = images.publish(value, output, self.sha, False)
                    self.assertEqual(value["image_id"], result["image_id"])
                    self.assertFalse(result["pushed"])
                    self.assertEqual(5, commands.call_count)

    def test_oci_blob_tampering_is_refused_even_with_a_resealed_archive_hash(self):
        value = self.fixture(layout="index")
        with tarfile.open(self.folder / value["archive"], "r:") as handle:
            names = [member.name for member in handle if member.name.startswith("blobs/")]
        for name in names:
            with self.subTest(blob=name):
                value = self.fixture(layout="index")
                self.rewrite(value, lambda contents: contents.__setitem__(
                    name, b"X" + contents[name][1:]))
                with patch.object(images, "run") as commands:
                    with self.assertRaisesRegex(images.ImageContractError, "descriptor digest mismatch"):
                        images.publish(value, self.folder, self.sha, False)
                commands.assert_not_called()

    def test_oci_index_requires_the_exact_inspected_id_tag_and_descriptor(self):
        changes = (
            lambda index: index["manifests"][0].update(digest="sha256:" + "0" * 64),
            lambda index: index["manifests"][0].update(size=0),
            lambda index: index["manifests"][0]["annotations"].update(
                {"io.containerd.image.name": "docker.io/library/unrelated:ci"}),
            lambda index: index["manifests"][0]["annotations"].update(
                {"org.opencontainers.image.ref.name": "unrelated"}),
            lambda index: index["manifests"].append(dict(index["manifests"][0])),
        )
        for change in changes:
            with self.subTest(change=change):
                value = self.fixture(layout="index")

                def update(contents):
                    index = json.loads(contents["index.json"])
                    change(index)
                    contents["index.json"] = json.dumps(index).encode()

                self.rewrite(value, update)
                with self.assertRaises(images.ImageContractError):
                    images.validate_archive(value, self.folder, self.sha)
        value = self.fixture(layout="index")
        value["image_id"] = "sha256:" + "0" * 64
        with self.assertRaises(images.ImageContractError):
            images.validate_archive(value, self.folder, self.sha)

    def test_oci_index_only_accepts_one_runtime_and_its_related_attestations(self):
        changes = (
            lambda index: index["manifests"].append(dict(index["manifests"][0])),
            lambda index: index["manifests"][0]["platform"].update(architecture="arm64"),
            lambda index: index["manifests"][0].update(platform=None),
            lambda index: index["manifests"].append(None),
            lambda index: index["manifests"][1]["annotations"].update(
                {"vnd.docker.reference.digest": "sha256:" + "0" * 64}),
            lambda index: index["manifests"][1]["annotations"].update(
                {"vnd.docker.reference.type": "unrelated"}),
            lambda index: index["manifests"][1].update(
                platform={"os": "linux", "architecture": "arm64"}),
        )
        for change in changes:
            with self.subTest(change=change):
                value = self.fixture(layout="index")
                self.rewrite_index(value, change)
                with self.assertRaises(images.ImageContractError):
                    images.validate_archive(value, self.folder, self.sha)

    def test_oci_and_docker_manifests_must_bind_the_same_config_and_layers(self):
        for field, replacement in (("Config", None), ("Layers", [])):
            with self.subTest(field=field):
                value = self.fixture(layout="index")

                def update(contents):
                    manifest = json.loads(contents["manifest.json"])
                    if field == "Config":
                        contents["unrelated.json"] = contents[manifest[0]["Config"]]
                        manifest[0][field] = "unrelated.json"
                    else:
                        manifest[0][field] = replacement
                    contents["manifest.json"] = json.dumps(manifest).encode()

                self.rewrite(value, update)
                with self.assertRaises(images.ImageContractError):
                    images.validate_archive(value, self.folder, self.sha)

    def test_oci_keeps_revision_platform_and_tag_checks(self):
        for layout in ("manifest", "index"):
            for options in (
                    {"tag": "unrelated:ci"},
                    {"config": {**self.config, "architecture": "arm64"}},
                    {"config": {**self.config, "os": "windows"}},
                    {"config": {"os": "linux", "architecture": "amd64", "config": {
                        "Labels": {"org.opencontainers.image.revision": "b" * 40}}}},
            ):
                with self.subTest(layout=layout, options=options):
                    value = self.fixture(layout=layout, **options)
                    with patch.object(images, "run") as commands:
                        with self.assertRaises(images.ImageContractError):
                            images.publish(value, self.folder, self.sha, False)
                    commands.assert_not_called()

    def test_duplicate_archive_members_are_rejected(self):
        value = self.fixture(layout="index")
        archive = self.folder / value["archive"]
        with tarfile.open(archive, "a") as handle:
            member = tarfile.TarInfo("index.json")
            member.size = 2
            handle.addfile(member, io.BytesIO(b"{}"))
        value["archive_sha256"] = digest(archive)
        with self.assertRaises(images.ImageContractError):
            images.validate_archive(value, self.folder, self.sha)

    def test_cli_reports_a_safe_contract_reason_without_retrying(self):
        stderr = io.StringIO()
        with patch.object(images, "main", side_effect=images.ImageContractError(
                "OCI descriptor digest mismatch")) as main, redirect_stderr(stderr):
            self.assertEqual(1, images.cli())
        self.assertIn("OCI descriptor digest mismatch", stderr.getvalue())
        self.assertIn("no rebuild or retry", stderr.getvalue())
        main.assert_called_once_with()
        stderr = io.StringIO()
        with patch.object(images, "main", side_effect=OSError("private input")), \
                redirect_stderr(stderr):
            self.assertEqual(1, images.cli())
        self.assertIn("Malformed or unreadable", stderr.getvalue())
        self.assertNotIn("private input", stderr.getvalue())

    def test_pack_exports_the_inspected_configuration_for_both_components(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                value = self.fixture(component=component)
                output = self.folder / "packed"
                labels = {"org.opencontainers.image.revision": self.sha}

                def docker(command, **options):
                    if command[:3] == ["docker", "image", "inspect"]:
                        stdout = json.dumps(value["image_id"]) + " " + json.dumps(labels)
                    elif command == ["docker", "image", "save", "-o",
                                     str(output / value["archive"]), images.TAGS[component]]:
                        shutil.copyfile(self.folder / value["archive"],
                                        output / value["archive"])
                        stdout = ""
                    else:
                        self.fail(f"Unexpected Docker command: {command}")
                    return subprocess.CompletedProcess(command, 0, stdout, "")

                with patch.object(images, "run", side_effect=docker) as commands:
                    packed = images.pack(component, output, self.sha)
                self.assertEqual(value, packed)
                self.assertEqual(value, load_json(output / f"{component}.json"))
                self.assertEqual(2, commands.call_count)
                self.assertEqual(digest(self.folder / value["archive"]),
                                 digest(output / value["archive"]))

    def test_pack_rejects_a_foreign_source_before_export(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                value = self.fixture(component=component)
                with patch.object(images, "inspect_image", return_value=(
                        value["image_id"],
                        {"org.opencontainers.image.revision": "b" * 40})), \
                        patch.object(images, "run") as commands:
                    with self.assertRaises(ValueError):
                        images.pack(component, self.folder / "packed", self.sha)
                commands.assert_not_called()

    def test_wrong_revision_and_archive_corruption(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                value = self.fixture(component=component)
                with patch.object(images, "run") as commands:
                    with self.assertRaises(ValueError):
                        images.publish(value, self.folder, "b" * 40, False)
                    with (self.folder / value["archive"]).open("ab") as handle:
                        handle.write(b"corrupted")
                    with self.assertRaises(ValueError):
                        images.publish(value, self.folder, self.sha, False)
                commands.assert_not_called()

    def test_retagged_or_foreign_image_refused(self):
        for component in ("api", "web"):
            for options in (
                    {"tag": "some-other-image:ci"},
                    {"config": {"os": "linux", "architecture": "amd64", "config": {}}},
                    {"config": {**self.config, "architecture": "arm64"}},
                    {"config": {**self.config, "os": "windows"}},
                    {"config": {"os": "linux", "architecture": "amd64", "config": {
                        "Labels": {"org.opencontainers.image.revision": "b" * 40}}}},
            ):
                with self.subTest(component=component, options=options):
                    value = self.fixture(component=component, **options)
                    with patch.object(images, "run") as commands:
                        with self.assertRaises(ValueError):
                            images.publish(value, self.folder, self.sha, False)
                    commands.assert_not_called()

    def test_configuration_digest_and_manifest_shape_are_verified(self):
        for component in ("api", "web"):
            with self.subTest(component=component, change="configuration ID"):
                value = self.fixture(component=component)
                value["image_id"] = "sha256:" + "0" * 64
                with self.assertRaises(ValueError):
                    images.validate_archive(value, self.folder, self.sha)
            for manifest in (
                    [],
                    [{}, {}],
                    [{"Config": "../config.json", "RepoTags": [images.TAGS[component]]}],
                    [{"Config": "/config.json", "RepoTags": [images.TAGS[component]]}],
                    [{"Config": "missing.json", "RepoTags": [images.TAGS[component]]}],
            ):
                with self.subTest(component=component, manifest=manifest):
                    value = self.fixture(component=component, manifest=manifest)
                    with patch.object(images, "run") as commands:
                        with self.assertRaises((ValueError, KeyError)):
                            images.publish(value, self.folder, self.sha, False)
                    commands.assert_not_called()

    def test_archive_symlink_is_refused_before_loading(self):
        value = self.fixture()
        archive = self.folder / value["archive"]
        original = self.folder / "original.tar"
        archive.rename(original)
        archive.symlink_to(original.name)
        with patch.object(images, "run") as commands:
            with self.assertRaises(ValueError):
                images.publish(value, self.folder, self.sha, False)
        commands.assert_not_called()

    def test_load_or_inspect_failure_prevents_tagging(self):
        for component in ("api", "web"):
            for failure in ("load", "inspect", "wrong-id"):
                with self.subTest(component=component, failure=failure):
                    value = self.fixture(component=component)
                    identifier = ("sha256:" + "0" * 64 if failure == "wrong-id"
                                  else value["image_id"])
                    responses = [
                        subprocess.CompletedProcess([], int(failure == "load"), "", ""),
                        subprocess.CompletedProcess(
                            [], int(failure == "inspect"), json.dumps(identifier) + " {}", ""),
                    ]
                    with patch.object(images, "run", side_effect=responses) as commands:
                        with self.assertRaises(ValueError):
                            images.publish(value, self.folder, self.sha, False)
                    self.assertEqual(1 if failure == "load" else 2, commands.call_count)
                    self.assertTrue(all(call.args[0][1] == "image"
                                        for call in commands.call_args_list))

    def test_publish_cli_rejects_another_checkout_before_docker(self):
        with patch("sys.argv", [
                "images.py", "publish", "--component", "api",
                "--directory", str(self.folder), "--sha", "b" * 40]), \
                patch.object(images, "revision", return_value=self.sha), \
                patch.object(images, "run") as commands:
            with self.assertRaises(ValueError):
                images.main()
        commands.assert_not_called()

    def test_publish_cli_refuses_another_component_before_loading(self):
        for requested, other in (("api", "web"), ("web", "api")):
            with self.subTest(requested=requested, metadata=other):
                value = self.fixture(component=other)
                write_json(self.folder / f"{requested}.json", value)
                responses = [
                    subprocess.CompletedProcess([], 0, "", ""),
                    subprocess.CompletedProcess([], 0, json.dumps(value["image_id"]) + " {}", ""),
                    subprocess.CompletedProcess([], 0, "", ""),
                ]
                with patch("sys.argv", [
                        "images.py", "publish", "--component", requested,
                        "--directory", str(self.folder)]), \
                        patch.object(images, "revision", return_value=self.sha), \
                        patch.object(images, "run", side_effect=responses) as commands, \
                        redirect_stdout(io.StringIO()):
                    with self.assertRaises(ValueError):
                        images.main()
                commands.assert_not_called()
