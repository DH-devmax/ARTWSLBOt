"""Evidence-based architecture snapshots; no product execution or database access."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit

import yaml

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
OUTPUT = ROOT / "docs/architecture/generated"
VIEWS = ("system", "backend", "frontend", "agents")


def run(args, root=ROOT, **kwargs):
    return subprocess.check_output(args, cwd=root, text=True, **kwargs)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def write_changed(path, content):
    if path.exists() and path.read_text() == content:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)
    return True


def json_text(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n"


def candidate_files(root=ROOT):
    raw = run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], root)
    result = []
    for name in sorted(set(filter(None, raw.split("\0")))):
        path = root / name
        if path.is_symlink():
            if name.startswith(("cmd/", "internal/", "web/src/wsl/")):
                raise ValueError("Product source symlink rejected: " + name)
            continue
        if path.is_file():
            result.append(name)
    return result


def input_hashes(root=ROOT):
    names = [n for n in candidate_files(root) if (
        n.startswith(("cmd/", "internal/", "web/src/wsl/", "tools/architecture/"))
        or n in ("compose.yaml", "go.mod", "go.sum", "web/tsconfig.json", "web/tsconfig.wsl.json",
                 "deploy/api.Dockerfile", "deploy/web.Dockerfile", "web/nginx.conf",
                 ".codeboarding/.codeboardingignore")
    ) and not n.endswith(".pyc")]
    return {n: hashlib.sha256((root / n).read_bytes()).hexdigest() for n in names}


def decode_stream(text):
    result = []
    decoder = json.JSONDecoder()
    while text.strip():
        value, end = decoder.raw_decode(text.lstrip())
        result.append(value)
        text = text.lstrip()[end:]
    return result


def assistant_present(candidates):
    required = {
        "web/src/wsl/assistant/client.ts",
        "internal/assistant/runtime.go",
        "internal/httpapi/assistant.go",
        "internal/control/assistant_turns.go",
        "internal/control/assistant_plans.go",
    }
    present = required & set(candidates)
    if present and present != required:
        raise ValueError("Partial assistant wiring; review missing anchors: " + ", ".join(sorted(required - present)))
    return bool(present)


def collect(root=ROOT):
    nodes, edges, gaps = [], [], []

    def node(id_, view, kind, **extra):
        if not any(n["id"] == id_ and n["view"] == view for n in nodes):
            nodes.append({"id": id_, "view": view, "kind": kind, **extra})

    def edge(from_, to, view, kind, evidence, **extra):
        if not evidence:
            raise ValueError("An edge without source evidence is not permitted")
        for existing in edges:
            if (existing["from"], existing["to"], existing["view"], existing["kind"]) == (from_, to, view, kind):
                unique = {json.dumps(e, sort_keys=True): e for e in existing["evidence"] + evidence}
                existing["evidence"] = sorted(unique.values(), key=lambda e: (e["path"], e["line"]))
                if extra.get("template_used"):
                    existing["template_used"] = True
                return
        edges.append({"from": from_, "to": to, "view": view, "kind": kind,
                      "evidence": sorted(evidence, key=lambda e: (e["path"], e["line"])), **extra})

    env = {**os.environ, "GOOS": "linux", "CGO_ENABLED": "1"}
    packages = decode_stream(run(["go", "list", "-json", "-tags", "sodium", "./cmd/...", "./internal/..."],
                                 root, env=env))
    module = packages[0]["Module"]["Path"]
    go_files = []
    for package in packages:
        directory = str(Path(package["Dir"]).relative_to(root))
        node(directory, "backend", "go-package")
        go_files.extend(str(Path(directory) / f) for f in package.get("GoFiles", []) + package.get("CgoFiles", []))
    facts = json.loads(run(["go", "run", str(HERE / "gofacts.go"), str(root)],
                          root, input=json.dumps(go_files)))
    candidates = set(candidate_files(root))
    if set(go_files) - candidates:
        raise ValueError("Build includes ignored/non-candidate Go source; review before documenting")
    for package in packages:
        source = str(Path(package["Dir"]).relative_to(root))
        for target in package.get("Imports", []):
            if target.startswith(module + "/"):
                target_path = target[len(module) + 1:]
                evidence = facts["imports"].get(source + "->" + target, [])
                edge(source, target_path, "backend", "imports", evidence)

    frontend = json.loads(run(["node", str(HERE / "frontend.mjs"), str(root)], root))
    for item in frontend["nodes"]:
        if item["id"] not in candidates:
            raise ValueError("Ignored/non-candidate product UI source: " + item["id"])
        node(item["id"], "frontend", item["kind"])
    for item in frontend["edges"]:
        node(item["to"], "frontend", "external" if item["external"] else "frontend")
        edge(item["from"], item["to"], "frontend", item["kind"], item["evidence"],
             external=item["external"], template_used=item["template_used"])
    gaps.extend(frontend["gaps"])

    compose_path = root / "compose.yaml"
    compose = yaml.safe_load(compose_path.read_text())
    compose_lines = compose_path.read_text().splitlines()
    for name, config in sorted(compose["services"].items()):
        declaration = next(i + 1 for i, text in enumerate(compose_lines) if text == f"  {name}:")
        node("service:" + name, "system", "compose-service",
             lifecycle="one-shot" if name in ("bootstrap", "migrate") else "runtime",
             generation_gate=config.get("environment", {}).get("AI_ALLOW_TEST_GENERATION"))
        for dependency in config.get("depends_on", {}):
            edge("service:" + name, "service:" + dependency, "system", "startup dependency",
                 [{"path": "compose.yaml", "line": declaration, "symbol": name + ".depends_on." + dependency}])
        for key in ("DATABASE_URL", "SIMON_URL"):
            value = config.get("environment", {}).get(key, "")
            # Do not interpolate dotenv or retain URLs/passwords in the graph.
            target = urlsplit(re.sub(r"\$\{[^}]*\}", "REDACTED", value)).hostname if value else None
            if target in compose["services"]:
                env_line = next(i + 1 for i, text in enumerate(compose_lines) if re.match(r"\s+" + key + ":", text))
                edge("service:" + name, "service:" + target, "system",
                     "database endpoint" if key == "DATABASE_URL" else "private metrics endpoint",
                     [{"path": "compose.yaml", "line": env_line, "symbol": name + ".environment." + key}])
    nginx = (root / "web/nginx.conf").read_text().splitlines()
    for index, line in enumerate(nginx):
        target = re.search(r"proxy_pass\s+http://([a-z0-9_-]+):\d+", line)
        if target and target[1] in compose["services"]:
            edge("service:web", "service:" + target[1], "system", "HTTP proxy",
                 [{"path": "web/nginx.conf", "line": index + 1, "symbol": "proxy_pass"}])

    def anchor(path, symbol):
        matches = [entry for entry in facts["symbols"].get(symbol, []) if entry["path"] == path]
        if not matches:
            raise ValueError(f"Agent wiring evidence disappeared: {path}:{symbol}; review the mapping")
        return matches

    # A reviewed mapping, not an LLM-inferred call graph. Missing anchors fail closed.
    node("heatbot", "agents", "agent-component", label="Heatbot endpoint (generation disabled)")
    node("heatbot-provider", "agents", "agent-component", label="Heatbot model adapter (gated)")
    edge("heatbot", "heatbot-provider", "agents", "Generate interface (endpoint disabled)",
         anchor("internal/heatbot/engine.go", "Generate") + anchor("internal/heatbot/provider.go", "Generate"))
    has_assistant = assistant_present(candidates)
    if not has_assistant:
        gaps.append({"kind": "coverage", "detail": "Management assistant is absent from this revision; no assistant interactions are drawn."})
    agent_nodes = {
        "ui": "Assistant drawer", "api": "HTTP API", "runtime": "Bounded assistant runtime",
        "provider": "Assistant model adapter", "host": "Control authorization and tools",
        "approval": "Human approval / shared configuration transaction",
        "heatbot": "Heatbot endpoint (generation disabled)",
    }
    for id_, label in agent_nodes.items() if has_assistant else []:
        node(id_, "agents", "agent-component", label=label)
    if has_assistant:
        ui_path = "web/src/wsl/assistant/client.ts"
        ui_source = (root / ui_path).read_text().splitlines()
        matches = [{"path": ui_path, "line": i + 1, "symbol": "/assistant"}
                   for i, text in enumerate(ui_source) if "/assistant" in text]
        edge("ui", "api", "agents", "authenticated API / SSE", matches)
        edge("api", "runtime", "agents", "Run interface",
             anchor("internal/httpapi/assistant.go", "Run") + anchor("internal/httpapi/server.go", "AssistantRuntime"))
        edge("runtime", "provider", "agents", "Generate interface",
             anchor("internal/assistant/runtime.go", "Generate") + anchor("internal/httpapi/server.go", "NewHTTPProvider"))
        edge("runtime", "host", "agents", "Host.Check / Call (not a package import)",
             anchor("internal/assistant/runtime.go", "Check") + anchor("internal/assistant/runtime.go", "Call")
             + anchor("internal/control/assistant_turns.go", "Check") + anchor("internal/control/assistant_turns.go", "Call"))
        edge("api", "approval", "agents", "explicit approval only",
             anchor("internal/httpapi/assistant.go", "Approve") + anchor("internal/control/assistant_plans.go", "Approve"))
    node("service:api", "system", "compose-service")
    if compose["services"]["heatbot"]["environment"].get("AI_ALLOW_TEST_GENERATION") != "false":
        raise ValueError("Heatbot generation gate changed; human architecture review required")
    gaps.append({"kind": "coverage", "detail": "Agent view is reviewed interface wiring, not whole-program dispatch analysis."})
    gaps.append({"kind": "coverage", "detail": "Compose dependencies are startup declarations, not observed network calls."})
    return {"schema_version": 1,
            "build_context": {"GOOS": "linux", "CGO_ENABLED": "1", "tags": ["sodium"]},
            "nodes": sorted(nodes, key=lambda n: (n["view"], n["id"])),
            "edges": sorted(edges, key=lambda e: (e["view"], e["from"], e["to"], e["kind"])),
            "coverage": sorted(gaps, key=lambda g: json.dumps(g, sort_keys=True))}


def view_structure(graph, view):
    nodes = [n for n in graph["nodes"] if n["view"] == view and n["kind"] != "external"]
    ids = {n["id"] for n in nodes}
    edges = [{k: v for k, v in e.items() if k not in ("evidence", "view")}
             for e in graph["edges"] if e["view"] == view and e["from"] in ids and e["to"] in ids]
    return {"nodes": nodes, "edges": edges}


def mermaid(graph, view):
    structure = view_structure(graph, view)
    ids = {n["id"]: "n" + hashlib.sha256(n["id"].encode()).hexdigest()[:12] for n in structure["nodes"]}
    def label(text):
        return re.sub(r'[^A-Za-z0-9 _./():=-]', "_", text)
    lines = ["%% Generated from source evidence. Do not hand edit.",
             "%% Import / declaration edges are not proof of runtime activation.",
             "flowchart TB" if view == "system" else "flowchart LR"]
    for item in structure["nodes"]:
        text = item.get("label", item["id"].removeprefix("web/src/wsl/"))
        if item.get("lifecycle") == "one-shot":
            text += " (one-shot)"
        if item.get("generation_gate") == "false":
            text += " (generation disabled)"
        lines.append(f'  {ids[item["id"]]}["{label(text)}"]')
    for item in structure["edges"]:
        lines.append(f'  {ids[item["from"]]} -->|"{label(item["kind"])}"| {ids[item["to"]]}')
    return "\n".join(lines) + "\n"


def snapshot(root=ROOT):
    graph = collect(root)
    manifest = {"schema_version": 1, "input_hashes": input_hashes(root),
                "source_kind": "git-candidate-including-uncommitted",
                "extractor": "go-list-linux-sodium + go-ast + typescript-vue-ast + compose-yaml",
                "views": {v: digest(view_structure(graph, v)) for v in VIEWS}}
    outputs = {"graph.json": json_text(graph), "manifest.json": json_text(manifest)}
    outputs.update({v + ".mmd": mermaid(graph, v) for v in VIEWS})
    outputs.update({v + ".md": f"# {v.title()} architecture\n\n"
                    "Generated from code evidence; not production acceptance.\n\n```mermaid\n"
                    + mermaid(graph, v) + "```\n" for v in VIEWS})
    return outputs


def compare_graphs(before, after):
    report = {}
    for view in VIEWS:
        def elements(graph):
            structure = view_structure(graph, view)
            return ({n["id"] for n in structure["nodes"]},
                    {(e["from"], e["to"], e["kind"]) for e in structure["edges"]})
        old_nodes, old_edges = elements(before)
        new_nodes, new_edges = elements(after)
        report[view] = {"added_nodes": sorted(new_nodes - old_nodes), "removed_nodes": sorted(old_nodes - new_nodes),
                        "added_edges": sorted(new_edges - old_edges), "removed_edges": sorted(old_edges - new_edges)}
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["update", "check", "diff"])
    parser.add_argument("--base", help="Exact Git comparison commit (diff only)")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--candidate", type=Path, default=OUTPUT / "graph.json", help="Candidate graph (diff only)")
    args = parser.parse_args()
    args.output = args.output or (ROOT / "output/architecture/diff" if args.command == "diff" else OUTPUT)
    if args.command == "diff":
        after = json.loads(args.candidate.read_text())
        before = {"nodes": [], "edges": []}
        baseline_status = "no-baseline"
        if args.base:
            try:
                baseline = run(["git", "show", args.base + ":docs/architecture/generated/graph.json"],
                               stderr=subprocess.DEVNULL)
            except subprocess.CalledProcessError:
                baseline_status = "baseline-unavailable"
            else:
                before = json.loads(baseline)
                baseline_status = "available"
        report = compare_graphs(before, after)
        write_changed(args.output / "diff.json", json_text({"baseline": baseline_status, "views": report}))
        lines = ["## Architecture diff", "", f"Baseline: `{baseline_status}`.",
                 "Readiness and production behavior are not inferred from this report.", ""]
        for view, differences in report.items():
            counts = ", ".join(f"{key}: {len(values)}" for key, values in differences.items())
            lines.append(f"- **{view}**: {counts}")
            for kind, values in differences.items():
                if values:
                    lines.extend(f"  {kind}: `{str(value)}`" for value in values[:30])
                    if len(values) > 30:
                        lines.append(f"  Truncated: {len(values) - 30} more entries in diff.json.")
            if any(differences.values()):
                lines.extend(["", "<details>", f"<summary>{view}: candidate diagram</summary>", "",
                              "```mermaid", mermaid(after, view).rstrip(), "```", "", "</details>", ""])
        if baseline_status != "available":
            lines.append("\nNo trustworthy prior graph exists; this is an initial inventory, not a measured architecture change.")
        text = "\n".join(lines) + "\n"
        write_changed(args.output / "diff.md", text)
        print(text)
        return
    outputs = snapshot()
    changed = []
    for name, content in outputs.items():
        path = args.output / name
        if args.command == "check":
            if not path.exists() or path.read_text() != content:
                changed.append(name)
        elif write_changed(path, content):
            changed.append(name)
    if args.command == "check" and changed:
        raise SystemExit("Stale architecture files: " + ", ".join(changed))
    print(json.dumps({"command": args.command, "changed": changed, "views": list(VIEWS)}))


if __name__ == "__main__":
    main()
