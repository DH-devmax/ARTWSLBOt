"""Check compiled Go dependencies and the supported Docker deployment contract."""
import json
import subprocess
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def main():
    result = subprocess.run(["go", "list", "-deps", "-json", "./internal/heatbot", "./internal/monitor"],
                            cwd=ROOT, check=True, capture_output=True, text=True)
    decoder, position, packages = json.JSONDecoder(), 0, {}
    while position < len(result.stdout):
        while position < len(result.stdout) and result.stdout[position].isspace():
            position += 1
        if position == len(result.stdout):
            break
        value, position = decoder.raw_decode(result.stdout, position)
        packages[value["ImportPath"]] = value
    for name, package in packages.items():
        if not name.startswith(("wangshangliao/internal/heatbot", "wangshangliao/internal/monitor")):
            continue
        for dependency in package.get("Deps", []):
            if dependency in ("os/exec", "plugin", "database/sql") or dependency.startswith(
                    ("wangshangliao/internal/native", "wangshangliao/internal/control",
                     "wangshangliao/internal/database", "wangshangliao/internal/httpapi",
                     "wangshangliao/internal/gateway", "github.com/jackc/")):
                raise SystemExit(f"Forbidden dependency: {name} -> {dependency}")
    compose = yaml.safe_load((ROOT / "compose.yaml").read_text())
    services = compose["services"]
    assert set(services) == {"postgres", "migrate", "bootstrap", "api", "web", "heatbot", "simon"}
    for name in ("api", "web", "heatbot", "simon"):
        service = services[name]
        assert service["read_only"] and service["cap_drop"] == ["ALL"], name
        assert service["security_opt"] == ["no-new-privileges:true"], name
        assert "privileged" not in service and service.get("pid") != "host", name
    simon = services["simon"]
    assert "@sha256:" in simon["image"]
    assert not simon.get("ports") and not simon.get("volumes")
    assert simon["networks"] == ["monitoring"] and compose["networks"]["monitoring"]["internal"]
    assert not services["heatbot"].get("ports") and not services["heatbot"].get("volumes")
    assert services["heatbot"]["environment"]["AI_ALLOW_TEST_GENERATION"] == "false"
    assert services["api"]["environment"]["ENABLE_NATIVE"] == "false"
    assert "MIGRATION_DATABASE_URL" not in services["api"]["environment"]
    dockerfile = (ROOT / "deploy/api.Dockerfile").read_text()
    assert "python" not in dockerfile.lower() and "COPY ai" not in dockerfile
    assert "cmd/heatbot" in dockerfile
    print("Architecture and Docker boundaries passed.")


if __name__ == "__main__":
    main()
