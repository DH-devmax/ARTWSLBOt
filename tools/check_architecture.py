"""Check compiled Go dependencies and the supported Docker deployment contract."""
import json
import subprocess
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def main():
    result = subprocess.run(["go", "list", "-deps", "-json", "./internal/heatbot", "./internal/monitor",
                             "./internal/features", "./internal/assistant", "./internal/aigateway",
                             "./internal/assistantadapter", "./internal/webcaptcha"],
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
        if name == "wangshangliao/internal/webcaptcha":
            for dependency in package.get("Deps", []):
                if dependency in ("os/exec", "plugin", "database/sql") or dependency.startswith((
                        "net/", "github.com/jackc/", "wangshangliao/internal/")):
                    raise SystemExit(f"Forbidden captcha dependency: {dependency}")
        if name in ("wangshangliao/internal/assistant","wangshangliao/internal/aigateway"):
            for dependency in package.get("Deps", []):
                if not packages[dependency].get("Standard") or dependency in (
                        "os/exec", "plugin", "database/sql"):
                    raise SystemExit(f"Forbidden assistant dependency: {name} -> {dependency}")
        if name == "wangshangliao/internal/assistantadapter":
            for dependency in package.get("Imports", []):
                if not packages[dependency].get("Standard") and not dependency.startswith((
                    "github.com/cloudwego/eino/", "github.com/eino-contrib/jsonschema",
                    "wangshangliao/internal/assistant"
                )):
                    raise SystemExit(f"Forbidden adapter import: {dependency}")
            for dependency in package.get("Deps", []):
                if dependency in ("os/exec","plugin","database/sql") or dependency.startswith((
                    "wangshangliao/internal/control","wangshangliao/internal/database",
                    "wangshangliao/internal/gateway","wangshangliao/internal/native",
                    "wangshangliao/internal/lajiaoliao","github.com/jackc/"
                )):
                    raise SystemExit(f"Forbidden adapter dependency: {dependency}")
        if name.startswith("wangshangliao/internal/features"):
            for dependency in package.get("Deps", []):
                if not packages[dependency].get("Standard") or dependency in (
                        "os/exec", "plugin", "database/sql") or dependency.startswith("net/"):
                    raise SystemExit(f"Forbidden feature dependency: {name} -> {dependency}")
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
    assert set(services) == {"postgres", "migrate", "bootstrap", "api", "web", "heatbot"}
    for name in ("api", "web", "heatbot"):
        service = services[name]
        assert service["read_only"] and service["cap_drop"] == ["ALL"], name
        assert service["security_opt"] == ["no-new-privileges:true"], name
        assert "privileged" not in service and service.get("pid") != "host", name
        assert not service.get("volumes"), name
    assert "SIMON_URL" not in services["api"]["environment"]
    assert "monitoring" not in compose.get("networks", {})
    metrics = ROOT / "internal/metricsproc"
    if metrics.exists():
        text = (metrics / "process_linux.go").read_text()
        assert "os.Getpid()" in text and "//go:build linux" in text
        assert ".MemoryInfoWithContext(" in text and "CLOCK_BOOTTIME" in text
        for forbidden in ("process.Processes", "process.Pids", ".Environ", ".Cmdline",
                          ".Connections", "exec.", "GetDocker", ".TimesWithContext(",
                          ".CreateTimeWithContext(", "process.NewProcess"):
            assert forbidden not in text, forbidden
        patched_cpu = (ROOT / "third_party/gopsutil/cpu/cpu.go").read_text()
        assert "func init()" not in patched_cpu
        assert "replace github.com/shirou/gopsutil/v4 => ./third_party/gopsutil" in (ROOT / "go.mod").read_text()
    assert not services["heatbot"].get("ports") and not services["heatbot"].get("volumes")
    assert services["heatbot"]["environment"]["AI_ALLOW_TEST_GENERATION"] == "false"
    assert services["api"]["environment"]["ENABLE_NATIVE"] == "false"
    assert "MIGRATION_DATABASE_URL" not in services["api"]["environment"]
    dockerfile = (ROOT / "deploy/api.Dockerfile").read_text()
    assert "python" not in dockerfile.lower() and "COPY ai" not in dockerfile
    assert "cmd/heatbot" in dockerfile
    auth = ROOT / "internal/nativeauth"
    for source in auth.glob("*.go"):
        if not source.name.endswith("_test.go"):
            text = source.read_text()
            assert not any(marker in text for marker in (
                "func (c *Client) android(", '"3.7.1"', '"3.7.3"'
            )), f"Unpinned legacy mobile adapter returned: {source.name}"
    protocol = (auth / "protocol.go").read_text()
    assert 'const DesktopProtocolVersion = "2.8.3"' in protocol
    assert 'const MobileProtocolVersion = "3.7.4"' in protocol
    mobile = (auth / "mobile.go").read_text()
    assert "func ResumeMobile(" in mobile and "func (c *Client) finishMobile(" in mobile
    assert 'saved.NIMID != ""' in mobile and 'saved.NIMToken != ""' in mobile
    assert "func (c *Client) mobileRequestParams(" in mobile
    assert '"native_unsupported_route"' in mobile
    for forbidden in ("NIMToken:", "NIMID:", '"/v1/user/get-nim-token"', "IMDial", "RefreshToken"):
        assert forbidden not in mobile, forbidden
    restore = (auth / "resume.go").read_text()
    assert "saved.ProtocolVersion != d.ProtocolVersion" in restore and "validateDeploymentMode(d, false)" in restore
    companion = (ROOT / "internal/gateway/mobile_search.go").read_text()
    assert "nativeauth.ResumeMobile(" in companion
    assert "native-mobile-session:" in companion
    assert "IMDial" not in companion and "Run(" not in companion
    print("Architecture and Docker boundaries passed.")


if __name__ == "__main__":
    main()
