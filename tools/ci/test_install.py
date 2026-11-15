import io
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import subprocess

from tools.ci.install import download, go_tool, ruff_binary


class InstallerTests(unittest.TestCase):
    def test_untrusted_url_or_checksum_rejected_before_network(self):
        with tempfile.TemporaryDirectory() as temporary, \
                patch("tools.ci.install.urlopen") as request:
            for asset in [{"url": "http://example.com/tool", "sha256": "a" * 64},
                          {"url": "https://github.com/tool", "sha256": "wrong"}]:
                with self.assertRaises(ValueError):
                    download(asset, Path(temporary) / "asset")
            request.assert_not_called()

    def test_archive_symlink_cannot_be_installed(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / "ruff.tar.gz"
            with tarfile.open(archive, "w:gz") as handle:
                entry = tarfile.TarInfo("ruff/ruff")
                entry.type = tarfile.SYMTYPE
                entry.linkname = "/outside"
                handle.addfile(entry)
            with self.assertRaises(ValueError):
                ruff_binary(archive, Path(temporary) / "ruff")

    def test_installs_only_verified_binary_without_extracting_paths(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / "ruff.tar.gz"
            with tarfile.open(archive, "w:gz") as handle:
                entry = tarfile.TarInfo("../../outside/ruff")
                entry.size = 9
                handle.addfile(entry, io.BytesIO(b"synthetic"))
            target = Path(temporary) / "tool"
            ruff_binary(archive, target)
            self.assertEqual(b"synthetic", target.read_bytes())
            self.assertEqual(0o755, target.stat().st_mode & 0o777)

    def test_go_module_checksum_mismatch_never_installs(self):
        specification = {"module": "example.com/tool", "module_version": "v1.0.0",
                         "package": "example.com/tool/cmd/tool", "module_sum": "h1:expected",
                         "go_mod_sum": "h1:expectedmod"}
        with patch("tools.ci.install.run", return_value=subprocess.CompletedProcess(
                [], 0, '{"Sum":"h1:wrong","GoModSum":"h1:wrong"}', "")) as command:
            with self.assertRaises(ValueError):
                go_tool("tool", specification, Path("/tmp"), {})
            self.assertEqual(1, command.call_count)
