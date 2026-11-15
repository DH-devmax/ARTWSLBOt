"""Run inventoried Go tests and emit JSON reports without printing test output.

Skip policy: {"schema":1,"entries":[{"package":"module/pkg","test":"TestName",
"profile":"unit","fingerprint":"64 lowercase hex","reason":"human explanation",
"performance":false}]}. Fingerprints bind the exact parent function source,
package and parent name. Child skips need their own exact test name entry.
Database skips additionally require performance=true; ordinary DB tests must run.
"""

import argparse
import hashlib
import importlib.util
import json
import math
import os
import re
import signal
import subprocess
import sys
from pathlib import Path
from urllib.parse import parse_qsl, unquote, urlsplit


ROOT = Path(__file__).resolve().parents[2]
TAGS = ["sodium"]
OWNED_ROOTS = ("./cmd/...", "./internal/...", "./tools/...")
DB_PACKAGES = ("internal/control", "internal/httpapi", "internal/gateway")
DB_KEYS = ("DATABASE_URL", "TENANT_DATABASE_URL", "MIGRATION_DATABASE_URL")
OPT_INS = ("WSL_REPLAY", "WSL_MONITOR_REPLAY", "WSL_CACHE_DB_BENCH",
           "WSL_CACHE_BENCH", "WSL_CACHE_SOAK", "WSL_CUSTOMER_CLOSED_PERFORMANCE",
           "WSL_BUSINESS_DISABLED_LOAD")
TEST_NAME = re.compile(r"(?:Test|Fuzz|Example)[^\s/]*\Z")
FUNCTION = re.compile(r"\bfunc\s+([^\W\d]\w*)\s*\(")
SOURCE_KEYS = ("GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles", "HFiles",
               "FFiles", "SFiles", "SwigFiles", "SwigCXXFiles", "SysoFiles",
               "EmbedFiles", "TestGoFiles", "XTestGoFiles", "TestEmbedFiles",
               "XTestEmbedFiles")


def load_coverage_module():
    name = "_wsl_ci_coverage"
    if name not in sys.modules:
        spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name("coverage.py"))
        module = importlib.util.module_from_spec(spec)
        sys.modules[name] = module
        spec.loader.exec_module(module)
    return sys.modules[name]


coverage = load_coverage_module()


class RunnerError(ValueError):
    """Only fixed diagnostics may cross the subprocess boundary."""


def require(condition, message):
    if not condition:
        raise RunnerError(message)


def capture(command, root, environment, timeout=1800):
    try:
        process = subprocess.Popen(command, cwd=root, env=environment, stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, text=True, encoding="utf-8",
                                   errors="strict", start_new_session=True)
    except (OSError, UnicodeError, subprocess.SubprocessError):
        raise RunnerError("command unavailable") from None
    try:
        stdout, _ = process.communicate(timeout=timeout)
        return subprocess.CompletedProcess(command, process.returncode, stdout)
    except BaseException as error:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        if isinstance(error, (OSError, UnicodeError, subprocess.SubprocessError)):
            raise RunnerError("command timed out or returned invalid text") from None
        raise


def checked(command, root, environment):
    result = capture(command, root, environment)
    require(result.returncode == 0, "discovery command failed")
    return result.stdout


def json_objects(text):
    decoder = json.JSONDecoder(object_pairs_hook=coverage.object_pairs)
    index = 0
    while index < len(text):
        if text[index].isspace():
            index += 1
            continue
        try:
            value, index = decoder.raw_decode(text, index)
        except ValueError:
            raise RunnerError("invalid Go JSON stream") from None
        require(isinstance(value, dict), "invalid Go JSON event")
        yield value


def validate_dsn(value, *, localhost=False):
    require(isinstance(value, str) and bool(value), "database URL absent")
    try:
        parsed = urlsplit(value)
        require(parsed.scheme in ("postgres", "postgresql") and bool(parsed.hostname)
                and parsed.fragment == "" and (parsed.port is None or parsed.port > 0),
                "invalid isolated database URL")
        require(unquote(parsed.path) == "/wsl_test", "database must be wsl_test")
        if localhost:
            require(parsed.hostname == "127.0.0.1", "test fixture requires localhost URL")
        query = parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True)
        seen = set()
        for key, item in query:
            key = key.lower()
            require(key not in seen, "duplicate database URL option")
            seen.add(key)
            if key in ("dbname", "database"):
                require(item == "wsl_test", "database override must be wsl_test")
            require(key not in ("service", "servicefile", "host", "hostaddr", "port",
                                "options"), "database URL contains a routing override")
    except (ValueError, UnicodeError):
        raise RunnerError("invalid isolated database URL") from None


def validate_database(root, environment):
    # The Go fixtures read this file themselves; validating just os.environ is unsafe.
    try:
        text = (Path(root) / ".env").read_text(encoding="utf-8")
    except (OSError, UnicodeError):
        raise RunnerError("isolated database fixture unavailable") from None
    values = {}
    for line in text.splitlines():
        if "=" not in line or line.startswith("#"):
            continue
        key, value = line.split("=", 1)
        if key in DB_KEYS:
            require(key not in values, "duplicate fixture database URL")
            values[key] = value
    for key in DB_KEYS:
        validate_dsn(values.get(key), localhost=True)
        if key in environment:
            validate_dsn(environment[key])
    if "PGDATABASE" in environment:
        require(environment["PGDATABASE"] == "wsl_test", "PGDATABASE must be wsl_test")
    require(not any(environment.get(key) for key in ("PGSERVICE", "PGSERVICEFILE", "PGOPTIONS")),
            "database environment contains a routing override")
    return coverage.file_hash(Path(root) / ".env")


def build_environment(environment, profile, package=None):
    result = dict(environment)
    result["GOFLAGS"] = "-mod=readonly"
    result["GOWORK"] = "off"
    result["WSL_INTEGRATION"] = "1" if profile == "database" and package != DB_PACKAGES[2] else "0"
    result["WSL_GATEWAY_INTEGRATION"] = "1" if profile == "database" and package == DB_PACKAGES[2] else "0"
    for key in OPT_INS:
        result[key] = "0" if key != "WSL_MONITOR_REPLAY" else ""
    return result


def mask_go(text):
    """Mask comments and Go literals while preserving offsets and newlines."""
    output = list(text)
    index = 0
    while index < len(text):
        start = index
        if text.startswith("//", index):
            end = text.find("\n", index)
            index = len(text) if end < 0 else end
        elif text.startswith("/*", index):
            end = text.find("*/", index + 2)
            require(end >= 0, "unterminated Go source comment")
            index = end + 2
        elif text[index] in ('"', "'", "`"):
            quote = text[index]
            index += 1
            while index < len(text):
                if text[index] == quote:
                    index += 1
                    break
                if quote != "`" and text[index] == "\\":
                    index += 1
                index += 1
            else:
                raise RunnerError("unterminated Go source literal")
        else:
            index += 1
            continue
        for offset in range(start, index):
            if text[offset] not in ("\r", "\n"):
                output[offset] = " "
    return "".join(output)


def test_sources(root, package):
    sources = {}
    for name in package.get("TestGoFiles", []) + package.get("XTestGoFiles", []):
        path = Path(package["Dir"]) / name
        require(path.resolve().is_relative_to(Path(root).resolve()), "test source outside repository")
        try:
            raw = path.read_bytes().decode("utf-8")
        except (OSError, UnicodeError):
            raise RunnerError("test source unavailable") from None
        masked = mask_go(raw)
        for match in FUNCTION.finditer(masked):
            name = match[1]
            if not TEST_NAME.fullmatch(name):
                continue
            opening = masked.find("{", match.end())
            require(opening >= 0, "test function body absent")
            depth = 1
            end = opening + 1
            while end < len(masked) and depth:
                depth += (masked[end] == "{") - (masked[end] == "}")
                end += 1
            require(depth == 0 and name not in sources, "ambiguous test function source")
            fingerprint = hashlib.sha256(
                (package["ImportPath"] + "\n" + name + "\n" + raw[match.start():end])
                .encode("utf-8")).hexdigest()
            sources[name] = {"file": path.resolve().relative_to(Path(root).resolve()).as_posix(),
                             "line": raw.count("\n", 0, match.start()) + 1,
                             "end_line": raw.count("\n", 0, end) + 1,
                             "sha256": coverage.file_hash(path),
                             "fingerprint": fingerprint}
    return sources


def package_sources(root, package, files, *, dependency=False):
    directory = Path(package.get("Dir", "")).resolve()
    require(directory.is_relative_to(Path(root).resolve()),
            "compiled source outside repository")
    for key in SOURCE_KEYS:
        if dependency and key.startswith(("Test", "XTest")):
            continue
        for filename in package.get(key, []):
            path = coverage.inside(directory, filename)
            relative = path.relative_to(Path(root).resolve()).as_posix()
            files[relative] = coverage.file_hash(path)


def discover_packages(root, environment, patterns):
    require(tuple(patterns) == OWNED_ROOTS, "discovery must use owned roots")
    raw = checked(["go", "list", "-deps", "-test", "-json", "-race", "-tags",
                   "sodium", *patterns], root, environment)
    packages, files = {}, {}
    for package in json_objects(raw):
        require(not package.get("Error") and not package.get("Incomplete")
                and not package.get("DepsErrors"), "Go package discovery failed")
        import_path = package.get("ImportPath", "")
        if package.get("ForTest") or (
                package.get("Name") == "main" and import_path.endswith(".test")):
            continue
        if package.get("DepOnly"):
            replacement = package.get("Module", {}).get("Replace", {})
            if replacement and not replacement.get("Version"):
                directory = Path(replacement.get("Dir", "")).resolve()
                require(directory.is_relative_to(Path(root).resolve())
                        and directory.relative_to(Path(root).resolve()).parts[:1]
                        == ("third_party",), "local replacement outside third_party")
                require(Path(package.get("Dir", "")).resolve().is_relative_to(directory),
                        "replacement package escapes its module")
                package_sources(root, package, files, dependency=True)
                for name in ("go.mod", "go.sum"):
                    path = directory / name
                    if path.is_file():
                        relative = path.relative_to(Path(root).resolve()).as_posix()
                        files[relative] = coverage.file_hash(path)
            continue
        directory = Path(package.get("Dir", "")).resolve()
        require(isinstance(import_path, str) and import_path not in packages
                and directory.is_relative_to(Path(root).resolve()),
                "invalid or duplicate Go package")
        relative = directory.relative_to(Path(root).resolve()).as_posix()
        require(relative.split("/", 1)[0] in ("cmd", "internal", "tools"),
                "Go package outside owned roots")
        package_sources(root, package, files)
        packages[import_path] = package
    require(bool(packages), "no Go packages discovered")
    for name in ("go.mod", "go.sum"):
        if (Path(root) / name).is_file():
            files[name] = coverage.file_hash(Path(root) / name)
    return packages, files


def discover(root, environment, patterns):
    packages, files = discover_packages(root, environment, patterns)
    inventory = {}
    # A single list command has the same build/race/toolchain selection as execution.
    listing = checked(["go", "test", "-race", "-tags", "sodium", "-count=1", "-json",
                       "-list", ".", *patterns], root, environment)
    listed_packages = set()
    source_by_package = {name: test_sources(root, package) for name, package in packages.items()}
    for event in json_objects(listing):
        if event.get("Action") == "build-output":
            continue
        name = event.get("Package")
        require(name in packages, "listing returned an unknown package")
        action = event.get("Action")
        require(action not in ("fail", "build-fail"), "test listing failed")
        if action in ("pass", "skip"):
            require(name not in listed_packages, "duplicate package listing")
            listed_packages.add(name)
        if action != "output":
            continue
        output = event.get("Output", "")
        require(isinstance(output, str), "invalid test listing output")
        for test in output.splitlines():
            if not TEST_NAME.fullmatch(test):
                continue
            require(test in source_by_package[name], "listed test source absent")
            key = (name, test)
            require(key not in inventory, "duplicate listed parent test")
            inventory[key] = source_by_package[name][test]
    require(listed_packages == set(packages), "package listing incomplete")
    require(bool(inventory), "no parent tests discovered")
    return packages, inventory, files


def load_policy(path, inventory, profile):
    if not Path(path).exists():
        return {}
    value = coverage.read_json(path)
    require(isinstance(value, dict) and set(value) == {"schema", "entries"}
            and type(value["schema"]) is int and value["schema"] == 1
            and isinstance(value["entries"], list),
            "invalid skip policy")
    policy = {}
    for entry in value["entries"]:
        require(isinstance(entry, dict)
                and set(entry) <= {"package", "test", "profile", "fingerprint", "reason", "performance"}
                and set(entry) >= {"package", "test", "profile", "fingerprint", "reason"},
                "invalid skip policy entry")
        require(entry["profile"] in ("unit", "database")
                and isinstance(entry["package"], str) and isinstance(entry["test"], str)
                and isinstance(entry["fingerprint"], str)
                and coverage.HEX.fullmatch(entry["fingerprint"]) is not None
                and isinstance(entry["reason"], str) and bool(entry["reason"].strip())
                and type(entry.get("performance", False)) is bool,
                "invalid skip policy fields")
        key = (entry["package"], entry["test"])
        if entry["profile"] != profile:
            continue
        require(key not in policy, "duplicate skip policy entry")
        parent = (entry["package"], entry["test"].split("/", 1)[0])
        require(parent in inventory, "skip policy references an absent test")
        require(entry["fingerprint"] == inventory[parent]["fingerprint"],
                "stale skip policy fingerprint")
        require(profile != "database" or entry.get("performance") is True,
                "database skips must explicitly flag performance")
        policy[key] = entry
    return policy


class Inventory:
    def __init__(self, expected, packages, policy, profile):
        self.expected = expected
        self.packages = set(packages)
        self.policy = policy
        self.profile = profile
        self.runs = set()
        self.terminals = {}
        self.package_runs = set()
        self.package_terminals = {}
        self.problems = []
        self.diagnostics = set()

    def problem(self, code):
        self.problems.append(code)

    def consume(self, event):
        package = event.get("Package")
        action = event.get("Action")
        if package not in self.packages:
            self.problem("unknown_package")
            return
        if action not in ("start", "run", "pause", "cont", "pass", "fail", "skip",
                          "output", "build-output", "build-fail"):
            self.problem("unknown_action")
            return
        if action in ("output", "build-output"):
            output = event.get("Output", "")
            if isinstance(output, str):
                for line in output.splitlines():
                    if line.startswith("panic: test timed out after "):
                        self.diagnostics.add("package_timeout")
                    elif line.startswith("panic:"):
                        self.diagnostics.add("test_panic")
                    elif line.startswith("fatal error:"):
                        self.diagnostics.add("runtime_fatal")
                    if "no space left on device" in line:
                        self.diagnostics.add("storage_exhausted")
        test = event.get("Test")
        if "Test" in event and (not isinstance(test, str) or not test):
            self.problem("invalid_test_name")
            return
        if test is None:
            if action == "start":
                if package in self.package_runs:
                    self.problem("duplicate_package_start")
                self.package_runs.add(package)
            elif action in ("pass", "fail", "skip", "build-fail"):
                if package in self.package_terminals:
                    self.problem("duplicate_package_terminal")
                self.package_terminals[package] = action
                if action in ("fail", "build-fail"):
                    self.problem("package_failure")
            return
        if not isinstance(test, str):
            self.problem("invalid_test_name")
            return
        parent = (package, test.split("/", 1)[0])
        key = (package, test)
        if parent not in self.expected:
            self.problem("unexpected_test")
        if action == "run":
            if key in self.runs:
                self.problem("duplicate_test_run")
            self.runs.add(key)
        elif action in ("pass", "fail", "skip"):
            if key in self.terminals:
                self.problem("duplicate_test_terminal")
            if key not in self.runs:
                self.problem("test_terminal_without_run")
            elapsed = event.get("Elapsed", 0)
            if type(elapsed) not in (int, float) or not math.isfinite(elapsed) or elapsed < 0:
                self.problem("invalid_test_elapsed")
                elapsed = 0
            record = {"id": package + ":" + test, "package": package, "name": test,
                      "status": {"pass": "passed", "fail": "failed", "skip": "skipped"}[action],
                      "elapsed": elapsed}
            if action == "skip":
                entry = self.policy.get(key)
                if not entry:
                    self.problem("unapproved_skip")
                else:
                    record["skip"] = {"reason": entry["reason"],
                                      "fingerprint": entry["fingerprint"],
                                      "performance": entry.get("performance", False)}
            if action == "fail":
                self.problem("child_failure" if "/" in test else "test_failure")
            self.terminals[key] = record

    def finish(self, returncode):
        if returncode:
            self.problem("go_test_exit")
        if set(self.package_terminals) != self.packages:
            self.problem("missing_package_terminal")
        for key in self.runs - self.terminals.keys():
            self.problem("missing_test_terminal")
        cases = []
        for key, source in sorted(self.expected.items()):
            case = dict(self.terminals.get(key, {"package": key[0], "name": key[1],
                                                 "id": key[0] + ":" + key[1], "status": "missing",
                                                 "started": key in self.runs}))
            case["source"] = source
            if case["status"] == "missing":
                self.problem("missing_parent")
            cases.append(case)
        children = [dict(record, source=self.expected.get((key[0], key[1].split("/", 1)[0])))
                    for key, record in sorted(self.terminals.items()) if "/" in key[1]]
        counts = {"expected": len(cases), "actual": sum(c["status"] != "missing" for c in cases),
                  "passed": sum(c["status"] == "passed" for c in cases),
                  "failed": sum(c["status"] == "failed" for c in cases),
                  "skipped": sum(c["status"] == "skipped" for c in cases),
                  "missing": sum(c["status"] == "missing" for c in cases),
                  "children": len(children),
                  "child_failures": sum(c["status"] == "failed" for c in children)}
        return {"status": "failed" if self.problems else "passed", "counts": counts,
                "cases": cases, "children": children,
                "errors": sorted(set(self.problems)), "exit_code": returncode,
                "diagnostics": sorted(self.diagnostics),
                "unfinished": [{"package": key[0], "name": key[1]}
                               for key in sorted(self.runs - self.terminals.keys())]}


def toolchain(root, environment):
    raw = checked(["go", "env", "-json", *coverage.BUILD_KEYS], root, environment)
    try:
        value = json.loads(raw, object_pairs_hook=coverage.object_pairs)
    except ValueError:
        raise RunnerError("invalid toolchain response") from None
    require(isinstance(value, dict) and set(value) == set(coverage.BUILD_KEYS)
            and all(isinstance(item, str) and item for item in value.values())
            and value["CGO_ENABLED"] == "1", "race/sodium toolchain unavailable")
    return value


def source_sha(root, environment, supplied=None):
    candidate = supplied or environment.get("GITHUB_SHA")
    if candidate:
        require(coverage.COMMIT.fullmatch(candidate) is not None, "invalid source SHA")
    try:
        result = capture(["git", "rev-parse", "--verify", "HEAD"], root, environment, 30)
    except RunnerError:
        require(bool(candidate), "source SHA unavailable; supply --sha in source-only images")
        return candidate
    actual = result.stdout.strip()
    if result.returncode == 0 and coverage.COMMIT.fullmatch(actual):
        require(not candidate or candidate == actual, "supplied source SHA differs from HEAD")
        return actual
    require(bool(candidate), "source SHA unavailable; supply --sha in source-only images")
    return candidate


def coverage_metadata(root, path, packages, snapshot, identity, profile, module):
    try:
        blocks = coverage.parse_profile(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError):
        raise RunnerError("coverage output absent or invalid") from None
    mapping = {}
    for block in blocks.values():
        package, _, basename = block.file.rpartition("/")
        require(package in packages, "coverage contains an unexecuted package")
        directory = Path(packages[package]["Dir"]).resolve()
        source = coverage.inside(directory, basename)
        relative = source.relative_to(Path(root).resolve()).as_posix()
        require(relative in snapshot, "coverage source absent from inventory")
        mapping[block.file] = {"path": relative, "sha256": snapshot[relative], "package": package}
    scope = {"profile": profile, "packages": sorted(packages)}
    if profile == "database":
        scope["database"] = "wsl_test"
    meta = dict(identity, schema=1, source_files=snapshot, files=mapping, scope=scope,
                tags=TAGS, race=True, mode="atomic", status="passed",
                cover_sha256=coverage.file_hash(path), module=module)
    coverage.validate_identity(meta)
    coverage.validate_scope(meta, blocks)
    return meta


def run_profile(profile, output, root=ROOT, skip_policy=None, sha=None, environment=None):
    root = Path(root).resolve()
    output = Path(output).resolve()
    require(profile in ("unit", "database"), "unknown test profile")
    environment = dict(os.environ if environment is None else environment)
    output.mkdir(parents=True, exist_ok=True)
    require(not (output / "report.json").exists()
            and not list(output.glob("*.cover*")), "test output already exists")
    report = {"schema": 1, "profile": profile, "status": "failed", "sha": None,
              "toolchain": None, "counts": {}, "cases": [], "children": [],
              "runs": [], "coverage": [], "errors": []}
    expected = {}
    wanted = set()
    try:
        fixture = validate_database(root, environment) if profile == "database" else None
        base_env = build_environment(environment, "unit")
        report["sha"] = source_sha(root, base_env, sha)
        report["toolchain"] = toolchain(root, base_env)
        packages, expected, snapshot = discover(root, base_env, list(OWNED_ROOTS))
        wanted = set(expected) if profile == "unit" else {
            key for key in expected if any(
                Path(packages[key[0]]["Dir"]).resolve() == root / relative
                for relative in DB_PACKAGES)}
        report["inventory"] = [{"package": key[0], "name": key[1], "source": source}
                               for key, source in sorted(expected.items())]
        report["source_digest"] = coverage.source_digest(snapshot)
        modules = {package.get("Module", {}).get("Path") for package in packages.values()}
        require(len(modules) == 1 and None not in modules, "ambiguous source module")
        module = next(iter(modules))
        identity = {key: report[key] for key in ("sha", "toolchain", "source_digest")}
        policy = load_policy(skip_policy or root / "tools/ci/go-skips.json", expected, profile)
        selected = ([("unit", list(OWNED_ROOTS), packages)] if profile == "unit" else [
            ("database-" + relative.rsplit("/", 1)[-1], ["./" + relative],
             {name: package for name, package in packages.items()
              if Path(package["Dir"]).resolve() == root / relative})
            for relative in DB_PACKAGES])
        for name, patterns, current in selected:
            require(bool(current), "required database package absent")
            selected_expected = {key: value for key, value in expected.items() if key[0] in current}
            require(bool(selected_expected), "executed scope has no parent tests")
            collector = Inventory(selected_expected, current, policy, profile)
            relative = str(Path(next(iter(current.values()))["Dir"]).resolve().relative_to(root))
            run_env = build_environment(environment, profile, relative)
            if profile == "database":
                require(validate_database(root, environment) == fixture,
                        "database fixture changed during run")
            path = output / (name + ".cover")
            command = ["go", "test", "-race", "-tags", "sodium", "-count=1", "-json",
                       "-covermode=atomic", "-coverprofile=" + str(path), "-timeout=15m"]
            if profile == "database":
                command += ["-p=1", "-parallel=1"]
            result = capture(command + patterns, root, run_env)
            try:
                for event in json_objects(result.stdout):
                    collector.consume(event)
            except RunnerError:
                collector.problem("invalid_go_test_json")
            run = collector.finish(result.returncode)
            run.update({"name": name, "packages": sorted(current)})
            report["runs"].append(run)
            report["cases"].extend(run["cases"])
            report["children"].extend(run["children"])
            report["errors"].extend(run["errors"])
            if run["status"] == "passed":
                meta = coverage_metadata(root, path, current, snapshot, identity, profile, module)
                sidecar = Path(str(path) + ".meta.json")
                coverage.write_json(sidecar, meta)
                report["coverage"].append({"file": path.name, "metadata": sidecar.name})
            # A failing/resetting suite must not feed a later database suite.
            if run["status"] != "passed":
                break
        for name, digest in snapshot.items():
            require(coverage.file_hash(coverage.inside(root, name)) == digest,
                    "source changed during test run")
        final_packages, final_snapshot = discover_packages(root, base_env, list(OWNED_ROOTS))
        require(set(final_packages) == set(packages) and final_snapshot == snapshot,
                "source inventory changed during test run")
        require(toolchain(root, base_env) == report["toolchain"], "toolchain changed during run")
        if profile == "database":
            require(validate_database(root, environment) == fixture,
                    "database fixture changed during run")
        if not report["errors"]:
            report["status"] = "passed"
    except KeyboardInterrupt:
        report["errors"].append("runner_cancelled")
    except (RunnerError, coverage.CoverageError):
        report["errors"].append("runner_validation_failed")
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        report["errors"].append("runner_artifact_failed")
    recorded = {(case["package"], case["name"]) for case in report["cases"]}
    for key in sorted(wanted - recorded):
        report["cases"].append({"package": key[0], "name": key[1], "status": "missing",
                                "id": key[0] + ":" + key[1], "source": expected[key],
                                "started": False})
        report["errors"].append("missing_parent")
    if report["errors"]:
        report["status"] = "failed"
    report["counts"] = {
        "expected": len(report["cases"]),
        "actual": sum(case["status"] != "missing" for case in report["cases"]),
        "passed": sum(case["status"] == "passed" for case in report["cases"]),
        "failed": sum(case["status"] == "failed" for case in report["cases"]),
        "skipped": sum(case["status"] == "skipped" for case in report["cases"]),
        "missing": sum(case["status"] == "missing" for case in report["cases"]),
        "children": len(report["children"]),
        "child_failures": sum(case["status"] == "failed" for case in report["children"]),
    }
    report["errors"] = sorted(set(report["errors"]))
    coverage.write_json(output / "report.json", report)
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=("unit", "database"), required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--skip-policy", type=Path)
    parser.add_argument("--sha", help="Commit SHA for source-only CI images without .git")
    args = parser.parse_args(argv)
    def cancel(signum, frame):
        raise KeyboardInterrupt

    previous = signal.signal(signal.SIGTERM, cancel)
    try:
        try:
            report = run_profile(args.profile, args.output, args.root, args.skip_policy, args.sha)
        except (RunnerError, coverage.CoverageError, OSError, ValueError, KeyboardInterrupt):
            print("Go test runner setup failed", file=sys.stderr)
            return 1
    finally:
        signal.signal(signal.SIGTERM, previous)
    print(json.dumps({key: report[key] for key in ("schema", "profile", "status", "counts")}))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
