import hashlib
import io
import json
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

from tools.ci import images
from tools.ci.common import digest, load_json, write_json


def image_fixture(folder, component, sha, *, tag=None, config=None, manifest=None):
    folder.mkdir(parents=True, exist_ok=True)
    if config is None:
        config = {
            "os": "linux", "architecture": "amd64",
            "config": {"Labels": {
                "org.opencontainers.image.revision": sha,
                "org.opencontainers.image.title": component,
            }},
            "rootfs": {"type": "layers", "diff_ids": []},
        }
    raw = json.dumps(config).encode()
    identifier = hashlib.sha256(raw).hexdigest()
    archive = folder / f"{component}.tar"
    if manifest is None:
        manifest = [{
            "Config": identifier + ".json",
            "RepoTags": [tag if tag is not None else images.TAGS[component]],
            "Layers": [],
        }]
    with tarfile.open(archive, "w") as handle:
        for name, body in [("manifest.json", json.dumps(manifest).encode()),
                           (identifier + ".json", raw)]:
            member = tarfile.TarInfo(name)
            member.size = len(body)
            handle.addfile(member, io.BytesIO(body))
    value = {
        "schema": 1, "sha": sha, "component": component,
        "tag": images.TAGS[component], "archive": archive.name,
        "archive_sha256": digest(archive), "image_id": "sha256:" + identifier,
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

    def test_verified_exact_archive(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                value = self.fixture(component=component)
                self.assertIs(value, images.validate_archive(value, self.folder, self.sha))

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
