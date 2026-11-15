import copy
import io
import json
import shutil
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

from tools.ci import foundation, images, release
from tools.ci.common import digest, load_json, write_json
from tools.ci.evidence import JOBS, REQUIRED, record
from tools.ci.test_images import image_fixture


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.sha = "a" * 40
        self.evidence = self.root / "foundation-input"
        self.folder = self.root / "release"
        self.needs = {job: {"result": "success"} for job in JOBS}
        archive_folder = self.folder / "images"
        for component in ("api", "web"):
            image_fixture(archive_folder, component, self.sha)
        for job in JOBS:
            folder = self.evidence / job
            for filename in REQUIRED[job]:
                path = folder / filename
                path.parent.mkdir(parents=True, exist_ok=True)
                if job == "images":
                    shutil.copyfile(archive_folder / filename, path)
                elif path.suffix == ".json":
                    write_json(path, {"sha": self.sha, "status": "passed"})
                else:
                    path.write_text("synthetic")
            if job == "architecture":
                path = folder / "rendered/system.svg"
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("<svg/>")
            record(job, folder, self.sha)
        self.attestation = foundation.validate(self.needs, self.evidence, self.sha)
        write_json(self.folder / "foundation.json", self.attestation)
        shutil.copytree(self.evidence / "images", self.folder / "receipt")

    def reseal_image_receipt(self):
        """Keep outer hashes intact when a test targets an inner provenance check."""
        for component in ("api", "web"):
            shutil.copyfile(self.folder / "images" / f"{component}.json",
                            self.folder / "receipt" / f"{component}.json")
        record("images", self.folder / "receipt", self.sha)
        self.attestation["receipts"]["images"] = digest(
            self.folder / "receipt/job-report.json")
        write_json(self.folder / "foundation.json", self.attestation)

    def test_complete_foundation_and_both_images_publish_in_dry_run(self):
        self.assertEqual(sorted(JOBS), self.attestation["jobs"])
        self.assertEqual(set(JOBS), set(self.attestation["receipts"]))
        for job in JOBS:
            self.assertEqual(digest(self.evidence / job / "job-report.json"),
                             self.attestation["receipts"][job])
        self.assertIsNone(release.validate(self.folder, self.sha))
        folder = self.folder / "images"
        values = {component: load_json(folder / f"{component}.json")
                  for component in ("api", "web")}
        self.assertNotEqual(values["api"]["image_id"], values["web"]["image_id"])

        def docker(command, **options):
            if command[:4] == ["docker", "image", "load", "-i"]:
                self.assertIn(command[4], [str(folder / value["archive"])
                                          for value in values.values()])
                stdout = ""
            elif command[:3] == ["docker", "image", "inspect"]:
                component = next(name for name in values if images.TAGS[name] == command[-1])
                labels = {"org.opencontainers.image.revision": self.sha}
                stdout = json.dumps(values[component]["image_id"]) + " " + json.dumps(labels)
            elif command[:2] == ["docker", "tag"]:
                component = next(name for name in values
                                 if values[name]["image_id"] == command[2])
                self.assertEqual(f"ghcr.io/dh-devmax/artwslbot-{component}:sha-{self.sha}",
                                 command[3])
                stdout = ""
            else:
                self.fail(f"Unexpected Docker command: {command}")
            return subprocess.CompletedProcess(command, 0, stdout, "")

        with patch.object(images, "run", side_effect=docker) as commands, \
                patch.object(images, "revision", return_value=self.sha):
            for component in ("api", "web"):
                stdout = io.StringIO()
                with patch("sys.argv", [
                        "images.py", "publish", "--component", component,
                        "--directory", str(folder)]), redirect_stdout(stdout):
                    self.assertEqual(0, images.main())
                result = load_json(folder / f"{component}-release.json")
                self.assertEqual(result, json.loads(stdout.getvalue()))
                self.assertEqual(values[component],
                                 {key: result[key] for key in values[component]})
                self.assertIs(False, result["pushed"])
                self.assertNotIn("registry_digest", result)
        expected_commands = []
        for component, value in values.items():
            expected_commands.extend([
                ["docker", "image", "load", "-i", str(folder / value["archive"])],
                ["docker", "image", "inspect", "--format",
                 "{{json .Id}} {{json .Config.Labels}}", images.TAGS[component]],
                ["docker", "tag", value["image_id"],
                 f"ghcr.io/dh-devmax/artwslbot-{component}:sha-{self.sha}"],
            ])
        self.assertEqual(expected_commands, [call.args[0] for call in commands.call_args_list])
        self.assertIsNone(release.validate(self.folder, self.sha))

    def test_missing_foundation_or_foreign_revision_cannot_publish(self):
        with self.assertRaises(OSError):
            release.validate(self.root / "empty", self.sha)
        with self.assertRaises(ValueError):
            release.validate(self.folder, "b" * 40)
        for change in ({"schema": 2}, {"sha": "b" * 40}, {"status": "failed"},
                       {"jobs": [job for job in JOBS if job != "unit"]}):
            with self.subTest(change=change):
                write_json(self.folder / "foundation.json", {**self.attestation, **change})
                with self.assertRaises(ValueError):
                    release.validate(self.folder, self.sha)

    def test_missing_or_tampered_release_artifacts_are_rejected(self):
        paths = ["receipt/job-report.json"]
        paths += [f"{folder}/{component}.{suffix}"
                  for component in ("api", "web")
                  for folder, suffix in (("receipt", "json"), ("images", "json"),
                                         ("images", "tar"))]
        for name in paths:
            path = self.folder / name
            original = path.read_bytes()
            with self.subTest(path=name, change="missing"):
                path.unlink()
                try:
                    with self.assertRaises((ValueError, OSError)):
                        release.validate(self.folder, self.sha)
                finally:
                    path.write_bytes(original)
            with self.subTest(path=name, change="tampered"):
                path.write_bytes(original + b" ")
                try:
                    with self.assertRaises(ValueError):
                        release.validate(self.folder, self.sha)
                finally:
                    path.write_bytes(original)

    def test_another_image_from_the_same_sha_cannot_replace_the_tested_archive(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                config = {
                    "os": "linux", "architecture": "amd64", "config": {"Labels": {
                        "org.opencontainers.image.revision": self.sha,
                        "org.opencontainers.image.title": "untested replacement",
                    }},
                }
                image_fixture(self.folder / "images", component, self.sha, config=config)
                with self.assertRaises(ValueError):
                    release.validate(self.folder, self.sha)
                image_fixture(self.folder / "images", component, self.sha)

    def test_foreign_image_source_is_rejected_even_with_consistent_outer_hashes(self):
        for component in ("api", "web"):
            with self.subTest(component=component):
                config = {
                    "os": "linux", "architecture": "amd64", "config": {"Labels": {
                        "org.opencontainers.image.revision": "b" * 40,
                    }},
                }
                image_fixture(self.folder / "images", component, self.sha, config=config)
                self.reseal_image_receipt()
                with self.assertRaises(ValueError):
                    release.validate(self.folder, self.sha)
                image_fixture(self.folder / "images", component, self.sha)
                self.reseal_image_receipt()

    def test_failed_or_foreign_images_receipt_cannot_release(self):
        path = self.folder / "receipt/job-report.json"
        original = load_json(path)
        for change in ({"schema": 2}, {"sha": "b" * 40}, {"status": "failed"},
                       {"job": "unit"}):
            with self.subTest(change=change):
                write_json(path, {**original, **change})
                self.attestation["receipts"]["images"] = digest(path)
                write_json(self.folder / "foundation.json", self.attestation)
                with self.assertRaises(ValueError):
                    release.validate(self.folder, self.sha)

    def test_missing_required_image_metadata_cannot_hide_in_a_resealed_receipt(self):
        path = self.folder / "receipt/job-report.json"
        original = load_json(path)
        for component in ("api", "web"):
            with self.subTest(component=component):
                receipt = copy.deepcopy(original)
                del receipt["files"][f"{component}.json"]
                write_json(path, receipt)
                self.attestation["receipts"]["images"] = digest(path)
                write_json(self.folder / "foundation.json", self.attestation)
                with self.assertRaises(ValueError):
                    release.validate(self.folder, self.sha)

    def test_foundation_with_only_images_receipt_is_incomplete(self):
        self.attestation["receipts"] = {
            "images": self.attestation["receipts"]["images"],
        }
        write_json(self.folder / "foundation.json", self.attestation)
        with self.assertRaises(ValueError):
            release.validate(self.folder, self.sha)

    def test_web_metadata_cannot_substitute_for_a_missing_api_image(self):
        folder = self.folder / "images"
        write_json(folder / "api.json", load_json(folder / "web.json"))
        (folder / "api.tar").unlink()
        self.reseal_image_receipt()
        with self.assertRaises(ValueError):
            release.validate(self.folder, self.sha)
