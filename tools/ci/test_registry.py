import copy
import tempfile
import unittest
from pathlib import Path

from tools.ci.registry import validate


class RegistryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "tools").mkdir()
        (self.root / "tools/test_one.py").write_text("synthetic")
        self.value = {"schema": 1, "python": ["tools/test_one.py"], "node": [],
                      "browser": [], "browser_component": [], "auxiliary": [], "manual": []}

    def test_complete_registry(self):
        self.assertIs(self.value, validate(self.value, self.root))

    def test_new_test_cannot_be_silently_omitted(self):
        (self.root / "tools/test_new.py").write_text("synthetic")
        with self.assertRaisesRegex(ValueError, "unregistered"):
            validate(self.value, self.root)

    def test_missing_test_cannot_be_silently_skipped(self):
        (self.root / "tools/test_one.py").unlink()
        with self.assertRaisesRegex(ValueError, "Missing"):
            validate(self.value, self.root)

    def test_duplicate_profile_registration(self):
        value = copy.deepcopy(self.value)
        value["node"].append("tools/test_one.py")
        with self.assertRaisesRegex(ValueError, "Duplicate"):
            validate(value, self.root)

    def test_live_script_needs_explicit_reason(self):
        value = copy.deepcopy(self.value)
        value["python"] = []
        value["manual"] = [{"path": "tools/test_one.py", "reason": ""}]
        with self.assertRaisesRegex(ValueError, "reason"):
            validate(value, self.root)
