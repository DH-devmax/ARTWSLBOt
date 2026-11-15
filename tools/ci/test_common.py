import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from tools.ci import common


class CommonTests(unittest.TestCase):
    def test_duplicate_json_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.json"
            path.write_text('{"sha":"a","sha":"b"}')
            with self.assertRaises(ValueError):
                common.load_json(path)

    def test_empty_or_duplicate_cases_are_not_success(self):
        with self.assertRaises(ValueError):
            common.report("unit", [])
        with self.assertRaises(ValueError):
            common.report("unit", [{"id": "x", "status": "passed"}] * 2)

    def test_child_failure_is_preserved(self):
        result = common.run([sys.executable, "-c", "raise SystemExit(7)"])
        self.assertEqual(7, result.returncode)

    def test_timeout_kills_process_group(self):
        with self.assertRaises(subprocess.TimeoutExpired):
            common.run([sys.executable, "-c", "import time; time.sleep(30)"], timeout=0.05)

    def test_cancel_kills_process_group(self):
        with patch("tools.ci.common.subprocess.Popen") as popen, \
                patch("tools.ci.common.os.killpg") as kill:
            child = popen.return_value
            child.pid = 1234
            child.communicate.side_effect = [KeyboardInterrupt(), ("", "")]
            with self.assertRaises(KeyboardInterrupt):
                common.run(["synthetic"])
            kill.assert_called_once()
