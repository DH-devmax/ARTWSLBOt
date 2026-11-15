import re
import tomllib
import unittest

from tools.ci.common import ROOT


class SecretPolicyTests(unittest.TestCase):
    def setUp(self):
        value = tomllib.loads((ROOT / ".gitleaks.toml").read_text())
        self.assertIs(value["extend"]["useDefault"], True)
        self.rules = value["rules"]
        self.assertEqual(["generic-api-key"], [rule["id"] for rule in self.rules])
        self.exceptions = self.rules[0]["allowlists"]

    def allowed(self, path, secret):
        for item in self.exceptions:
            self.assertEqual("AND", item["condition"])
            self.assertEqual("secret", item["regexTarget"])
            self.assertEqual(
                {"description", "condition", "paths", "regexTarget", "regexes"},
                set(item),
            )
            if (any(re.search(pattern, path) for pattern in item["paths"])
                    and any(re.search(pattern, secret) for pattern in item["regexes"])):
                return True
        return False

    def test_only_generated_checksums_and_fixed_fixture_tokens_are_exempt(self):
        self.assertTrue(self.allowed("docs/migrations.lock.json", "a" * 64))
        self.assertTrue(self.allowed("docs/architecture/generated/manifest.json", "b" * 64))
        self.assertTrue(self.allowed("internal/httpapi/integration_test.go", "fixture-finance-001"))
        self.assertTrue(self.allowed("internal/httpapi/integration_test.go", "fixture-finance-002"))

    def test_checksums_in_unrelated_files_remain_scanned(self):
        for path in ("web/src/wsl/api.ts", "internal/nativeauth/client.go",
                     "docs/architecture/generated/graph.json",
                     "copy/docs/architecture/generated/manifest.json",
                     "docs/architecture/generated/manifest.json.bak"):
            with self.subTest(path=path):
                self.assertFalse(self.allowed(path, "b" * 64))

    def test_credential_formats_and_near_matches_are_not_exempt(self):
        for secret in ("s" + "k-" + "z" * 48, "A" * 64, "f" * 63, "f" * 65,
                       "fixture-finance-003", "prefix" + "c" * 64):
            with self.subTest(length=len(secret)):
                self.assertFalse(self.allowed(
                    "docs/architecture/generated/manifest.json", secret))

    def test_fixture_exception_cannot_expand_to_other_keys(self):
        for secret in ("fixture-finance-003", "synthetic" + "x" * 32, "a" * 64):
            with self.subTest(length=len(secret)):
                self.assertFalse(self.allowed("internal/httpapi/integration_test.go", secret))

    def test_public_literals_and_synthetic_password_are_exact_and_path_bound(self):
        fixtures = (
            ("docs/test-report.md", "acceptance-observations-20261031"),
            ("internal/heatbot/gateway.go", "8080/internal/v1/ai/generate"),
            ("internal/httpapi/native_security_test.go", "private-new12"),
        )
        for path, value in fixtures:
            with self.subTest(path=path):
                self.assertTrue(self.allowed(path, value))
                self.assertFalse(self.allowed(path + ".bak", value))
                self.assertFalse(self.allowed("copy/" + path, value))
                self.assertFalse(self.allowed(path, value + "-changed"))
                self.assertFalse(self.allowed(path, "s" + "k-" + "z" * 48))
                self.assertFalse(self.allowed("internal/nativeauth/client.go", value))
