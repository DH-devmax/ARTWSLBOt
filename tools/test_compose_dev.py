"""Check the opt-in Docker development surface without starting services."""

import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


class DevelopmentComposeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dev = yaml.safe_load((ROOT / "compose.dev.yaml").read_text())
        cls.base = yaml.safe_load((ROOT / "compose.yaml").read_text())
        cls.image = (ROOT / "deploy/web.dev.Dockerfile").read_text()
        cls.vite = (ROOT / "web/vite.dev.config.ts").read_text()
        cls.helper = (ROOT / "tools/docker_dev.sh").read_text()

    def test_overlay_only_changes_frontend(self):
        self.assertEqual(set(self.dev), {"services", "volumes"})
        self.assertEqual(set(self.dev["services"]), {"web"})
        self.assertEqual(set(self.dev["volumes"]), {"web-dev-cache"})

    def test_source_mounts_are_read_only_and_no_secrets(self):
        mounts = self.dev["services"]["web"]["volumes"]
        self.assertEqual(len(mounts), 10)
        for source in mounts:
            if source.startswith("./"):
                self.assertTrue(source.endswith(":ro"))
                self.assertTrue(source.startswith("./web/"))
                self.assertNotIn(".env", source)
                self.assertNotIn("node_modules", source)
            else:
                self.assertEqual(source, "web-dev-cache:/home/node/.cache/wsl-vite")

    def test_no_host_permissions_or_extra_ports(self):
        web = self.dev["services"]["web"]
        for forbidden in ("ports", "privileged", "pid", "network_mode", "cap_add"):
            self.assertNotIn(forbidden, web)
        self.assertEqual(web["mem_limit"], "512m")
        self.assertEqual(web["memswap_limit"], "512m")
        self.assertEqual(web["environment"]["GOMEMLIMIT"], "128MiB")
        self.assertEqual(web["environment"]["GOMAXPROCS"], "2")
        self.assertTrue(self.base["services"]["web"]["read_only"])
        self.assertTrue(self.base["services"]["web"]["ports"][0].startswith("127.0.0.1:"))

    def test_image_does_not_copy_runtime_or_framework_demo(self):
        self.assertIn("USER node", self.image)
        self.assertIn("COPY src/wsl ./src/wsl", self.image)
        self.assertIn("COPY src/components/core/charts/art-bar-chart", self.image)
        self.assertIn("COPY src/components/core/charts/art-ring-chart", self.image)
        self.assertNotIn("COPY src/components ./src/components", self.image)
        self.assertNotIn("COPY .", self.image)
        self.assertNotIn("COPY src ./src", self.image)
        self.assertNotIn(".env", "\n".join(
            line for line in self.image.splitlines() if not line.startswith("#")
        ))
        self.assertIn("--ignore-scripts", self.image)
        self.assertIn("ENV NODE_ENV=development", self.image)
        self.assertIn("vite.js optimize --config vite.dev.config.ts", self.image)

    def test_vite_is_product_scoped_without_generated_declarations(self):
        self.assertIn("envDir: false", self.vite)
        self.assertIn("strictPort: true", self.vite)
        self.assertIn("changeOrigin: false", self.vite)
        self.assertIn("timeout: 105000", self.vite)
        self.assertNotIn("AutoImport", self.vite)
        self.assertNotIn("vueDevTools", self.vite)
        self.assertIn("**/.env*", self.vite)
        self.assertIn("response.statusCode = 403", self.vite)
        self.assertIn("preprocessorMaxWorkers: 0", self.vite)
        self.assertIn("WSL_DEV_POLLING === 'true'", self.vite)

    def test_helpers_require_explicit_api_restart(self):
        self.assertIn("--confirm-api-restart", self.helper)
        self.assertIn("WSL_API_IMAGE=wsl-api:dev", self.helper)
        self.assertIn("--no-deps --no-build --wait web", self.helper)
        self.assertIn("--no-deps --no-build --wait api", self.helper)
        self.assertIn("com.docker.compose.project.working_dir", self.helper)
        for forbidden in ("down -v", "volume prune", "system prune", "bootstrap", "wsl init"):
            self.assertNotIn(forbidden, self.helper)

    def test_agent_rules_separate_hmr_from_native_acceptance(self):
        rules = (ROOT / "AGENTS.md").read_text()
        for boundary in (
            "Docker frontend HMR", "Owner standing authorization (2026-10-29)",
            "restart only affected services", "not a watcher restarting on every edit",
            "Frontend-only HMR never restarts API/native connections",
            "unresolved submissions or failed checks", "incompatible migrations",
            "Never raise readiness merely because a deployment or HMR succeeds",
        ):
            self.assertIn(boundary, rules)


if __name__ == "__main__":
    unittest.main()
