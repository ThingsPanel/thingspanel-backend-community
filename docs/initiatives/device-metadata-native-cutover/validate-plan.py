#!/usr/bin/env python3
"""Read-only cross-reference validator for the native-cutover planning bundle.

This validates planning artifacts, not product implementation or execution.
Requires PyYAML. It never rewrites a planning file and performs no network I/O.
"""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

import yaml


HTTP_METHODS = {"get", "post", "put", "patch", "delete", "head", "options", "trace", "query"}
CLASSIFICATIONS = {
    "新系统计划完整实现",
    "已有 Encore 代码可复用，但未完成验收",
    "属于其他明确负责的运行时服务",
    "经产品边界确认后不迁移",
    "需要业务方确认",
}
TASK_PATTERN = re.compile(r"DM-[A-Z]+-\d{3}$")


class Audit:
    def __init__(self):
        self.errors = []
        self.warnings = []
        self.checks = Counter()

    def check(self, condition, code, detail):
        self.checks[code] += 1
        if not condition:
            self.errors.append({"check": code, "detail": detail})

    def warn(self, code, detail):
        self.warnings.append({"check": code, "detail": detail})


def walk(value):
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def local_ref(spec, value):
    """Resolve a JSON Pointer reference object, including chained references."""
    seen = set()
    while isinstance(value, dict) and "$ref" in value:
        ref = value["$ref"]
        if not ref.startswith("#/") or ref in seen:
            return value
        seen.add(ref)
        cur = spec
        for part in ref[2:].split("/"):
            cur = cur[part.replace("~1", "/").replace("~0", "~")]
        value = cur
    return value


def schema_refs(value):
    return {
        v["$ref"].rsplit("/", 1)[-1]
        for v in walk(value)
        if isinstance(v, dict)
        and isinstance(v.get("$ref"), str)
        and v["$ref"].startswith("#/components/schemas/")
    }


def duplicate_values(values):
    return sorted(str(k) for k, n in Counter(values).items() if n > 1)


def table_rows(text, first_cell_pattern):
    result = []
    for lineno, line in enumerate(text.splitlines(), 1):
        if not line.lstrip().startswith("|"):
            continue
        cells = [p.strip() for p in re.split(r"(?<!\\)\|", line.strip())[1:-1]]
        if cells and re.fullmatch(first_cell_pattern, cells[0]):
            result.append((lineno, cells))
    return result


def table_header_columns(text, lineno):
    for line in reversed(text.splitlines()[:lineno - 1]):
        if not line.lstrip().startswith("|"):
            continue
        cells = [p.strip() for p in re.split(r"(?<!\\)\|", line.strip())[1:-1]]
        if cells and all(re.fullmatch(r":?-{3,}:?", cell) for cell in cells):
            return len(cells)
    return None


def read_inputs(directory):
    files = ["tasks.json", "test-cases.json", "api-operations.json", "legacy-routes.json", "openapi.yaml"]
    raw = {name: (directory / name).read_bytes() for name in files}
    parsed = {name: (yaml.safe_load(data) if name.endswith(".yaml") else json.loads(data)) for name, data in raw.items()}
    doc_bytes = {p.name: p.read_bytes() for p in directory.glob("*.md")}
    docs = {name: data.decode("utf-8") for name, data in doc_bytes.items()}
    fingerprints = {name: hashlib.sha256(data).hexdigest() for name, data in raw.items()}
    fingerprints.update({name: hashlib.sha256(data).hexdigest() for name, data in doc_bytes.items()})
    return parsed, docs, fingerprints


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", nargs="?", default=str(Path(__file__).resolve().parent))
    parser.add_argument("--baseline", default="plan-original")
    parser.add_argument("--expected-tasks", type=int, default=188)
    parser.add_argument("--expected-routes", type=int, default=165)
    parser.add_argument("--expected-fei", type=int, default=139)
    parser.add_argument("--expected-internal", type=int, default=5)
    parser.add_argument("--expected-operations", type=int)
    parser.add_argument("--expected-cases", type=int)
    args = parser.parse_args()
    directory = Path(args.directory).resolve()
    audit = Audit()
    parsed, docs, fingerprints = read_inputs(directory)
    tasks = parsed["tasks.json"]
    cases = parsed["test-cases.json"]
    index = parsed["api-operations.json"]
    routes = parsed["legacy-routes.json"]
    spec = parsed["openapi.yaml"]
    by_task = {t["id"]: t for t in tasks}
    schemas = spec["components"]["schemas"]
    counts = {"tasks": len(tasks), "test_cases": len(cases), "operation_index": len(index), "legacy_routes": len(routes)}

    # Tasks, stable IDs, dependency DAG and release-gate reachability.
    required_task = {"id", "module", "title", "why", "scope", "tests", "accept", "deps", "priority", "state", "evidence", "risk", "operations", "tables", "blocking"}
    audit.check(len(tasks) == args.expected_tasks, "task_count", f"expected {args.expected_tasks}, actual {len(tasks)}")
    audit.check(not duplicate_values(t["id"] for t in tasks), "task_id_unique", duplicate_values(t["id"] for t in tasks))
    for task in tasks:
        tid = task["id"]
        audit.check(not (required_task - task.keys()), "task_required_fields", {"id": tid, "missing": sorted(required_task - task.keys())})
        audit.check(bool(TASK_PATTERN.fullmatch(tid)), "task_id_format", tid)
        for key in ["module", "title", "why", "scope", "accept", "state", "evidence", "risk"]:
            audit.check(isinstance(task.get(key), str) and bool(task[key].strip()), "task_nonempty", {"id": tid, "field": key})
        for key in ["tests", "deps", "operations", "tables"]:
            audit.check(isinstance(task.get(key), list), "task_array_field", {"id": tid, "field": key})
        audit.check(bool(task.get("tests")) and all(isinstance(t, str) and t.strip() for t in task["tests"]), "task_tests_nonempty", tid)
        audit.check(isinstance(task.get("blocking"), bool), "task_blocking_bool", tid)
        audit.check(task.get("priority") in {"P0", "P1", "P2"}, "task_priority", {"id": tid, "priority": task.get("priority")})
        audit.check(bool(re.match(r"^(已有代码|部分实现|待做|待核实|已撤回)", task.get("state", ""))), "task_status_vocabulary", {"id": tid, "state": task.get("state")})
        if task.get("state", "").startswith("已撤回"):
            audit.check(task.get("blocking") is False, "withdrawn_task_nonblocking", tid)
        for dep in task.get("deps", []):
            audit.check(dep in by_task, "task_dependency_exists", {"id": tid, "dependency": dep})
    active, visited, cycles = [], set(), []

    def visit(tid):
        if tid in active:
            cycles.append(active[active.index(tid):] + [tid])
            return
        if tid in visited or tid not in by_task:
            return
        active.append(tid)
        for dep in by_task[tid]["deps"]:
            visit(dep)
        active.pop()
        visited.add(tid)

    for tid in by_task:
        visit(tid)
    audit.check(not cycles, "task_dependency_dag", cycles)
    closure = set()
    pending = ["DM-QA-010"]
    while pending:
        tid = pending.pop()
        if tid in closure or tid not in by_task:
            continue
        closure.add(tid)
        pending.extend(by_task[tid]["deps"])
    counts["qa010_dependency_closure_including_self"] = len(closure)
    blocking = {t["id"] for t in tasks if t["blocking"]}
    audit.check(blocking <= closure, "qa010_all_blocking_task_closure", sorted(blocking - closure))
    counts["deferred_or_excluded_nonblocking"] = len(tasks) - len(blocking)
    rows = table_rows(docs.get("TASKS.md", ""), r"DM-[A-Z]+-\d{3}")
    counts["task_table_rows"] = len(rows)
    audit.check(len(rows) == len(tasks), "task_table_count", {"table": len(rows), "json": len(tasks)})
    audit.check(Counter(r[1][0] for r in rows) == Counter(by_task.keys()), "task_table_ids", {"missing": sorted(set(by_task) - {r[1][0] for r in rows}), "duplicates": duplicate_values(r[1][0] for r in rows)})
    for lineno, cells in rows:
        audit.check(len(cells) == 11, "task_table_column_count", {"line": lineno, "id": cells[0], "columns": len(cells)})
        tid = cells[0]
        if tid in by_task:
            for n, scenario in enumerate(by_task[tid]["tests"], 1):
                audit.check(f"TC-{tid}-{n:02}" in "|".join(cells), "task_table_test_id", f"TC-{tid}-{n:02}")

    # OpenAPI operations, JSON Pointer references and machine operation index.
    operations = {}
    for path, item in spec.get("paths", {}).items():
        for method, operation in item.items():
            if method not in HTTP_METHODS:
                continue
            oid = operation.get("operationId")
            audit.check(bool(oid), "openapi_operation_id_present", f"{method.upper()} {path}")
            audit.check(oid not in operations, "openapi_operation_id_unique", oid)
            operations[oid] = {"path": path, "method": method.upper(), **operation}
            all_params = item.get("parameters", []) + operation.get("parameters", [])
            names = {local_ref(spec, p).get("name") for p in all_params if local_ref(spec, p).get("in") == "path"}
            expected = set(re.findall(r"\{([^{}]+)\}", path))
            audit.check(names == expected, "openapi_path_parameters", {"operation": oid, "expected": sorted(expected), "actual": sorted(names)})
    counts["openapi_operations"] = len(operations)
    if args.expected_operations is not None:
        audit.check(len(operations) == args.expected_operations, "expected_operation_count", {"expected": args.expected_operations, "actual": len(operations)})
    for node in walk(spec):
        if isinstance(node, dict) and isinstance(node.get("$ref"), str) and node["$ref"].startswith("#/"):
            try:
                local_ref(spec, node)
            except (KeyError, TypeError, IndexError) as exc:
                audit.check(False, "openapi_ref_resolves", {"ref": node["$ref"], "error": str(exc)})
    by_operation = {x["id"]: x for x in index}
    audit.check(not duplicate_values(i["id"] for i in index), "operation_index_unique", duplicate_values(i["id"] for i in index))
    audit.check(set(by_operation) == set(operations), "operation_index_coverage", {"missing": sorted(set(operations) - set(by_operation)), "extra": sorted(set(by_operation) - set(operations))})
    for item in index:
        oid = item["id"]
        if oid not in operations:
            continue
        op = operations[oid]
        for left, right in [("path", "path"), ("method", "method"), ("owner", "x-owner"), ("permission", "x-permission"), ("task", "x-release-task")]:
            audit.check(item.get(left) == op.get(right), "operation_index_field", {"operation": oid, "field": left, "index": item.get(left), "spec": op.get(right)})
        audit.check(item.get("audience", "public") == op.get("x-audience", "public"), "operation_audience_index", oid)
        if op.get("x-audience") == "internal-service":
            audit.check(item.get("clients") == op.get("x-client-surfaces"), "operation_clients_index", oid)
        audit.check(item.get("task") in by_task, "operation_release_task", {"operation": oid, "task": item.get("task")})
        if item.get("task") in by_task:
            audit.check(oid in by_task[item["task"]]["operations"], "operation_in_primary_task", {"operation": oid, "task": item["task"]})
        request = local_ref(spec, op.get("requestBody", {}))
        request_names = schema_refs(request.get("content", {}))
        audit.check(item.get("request") in request_names if request_names else item.get("request") is None, "operation_request_schema", {"operation": oid, "index": item.get("request"), "spec": sorted(request_names)})
        success = op.get("responses", {}).get(str(item.get("status")), {})
        audit.check(bool(success), "operation_success_status", {"operation": oid, "status": item.get("status")})
        success = local_ref(spec, success)
        content = success.get("content", {})
        response_names = schema_refs(content)
        response_name = item.get("response")
        if response_name in schemas:
            audit.check(response_name in response_names, "operation_response_schema", {"operation": oid, "index": response_name, "spec": sorted(response_names)})
        elif response_name == "SSE ObservationEvent":
            audit.check("text/event-stream" in content, "operation_sse_media", oid)
        elif isinstance(response_name, str) and ("binary" in response_name or "download" in response_name):
            audit.check(bool(content) and any(k != "application/json" for k in content), "operation_download_media", {"operation": oid, "content": sorted(content)})
        else:
            audit.check(False, "operation_unknown_response_index", {"operation": oid, "response": response_name})
    for task in tasks:
        for oid in task["operations"]:
            audit.check(oid in operations, "task_operation_exists", {"task": task["id"], "operation": oid})

    # Each task's executable test specifications and contract references.
    audit.check(not duplicate_values(c["id"] for c in cases), "test_case_id_unique", duplicate_values(c["id"] for c in cases))
    counts["task_declared_cases"] = sum(len(t["tests"]) for t in tasks)
    if args.expected_cases is not None:
        audit.check(len(cases) == args.expected_cases, "expected_case_count", {"expected": args.expected_cases, "actual": len(cases)})
    expected_case_ids = {f"TC-{t['id']}-{n:02}" for t in tasks for n in range(1, len(t["tests"]) + 1)}
    audit.check({c["id"] for c in cases} == expected_case_ids, "test_case_complete_task_mapping", {"missing": sorted(expected_case_ids - {c["id"] for c in cases}), "extra": sorted({c["id"] for c in cases} - expected_case_ids)})
    for case in cases:
        cid, tid = case["id"], case.get("taskId")
        audit.check(tid in by_task, "test_task_exists", {"id": cid, "task": tid})
        if tid not in by_task:
            continue
        task = by_task[tid]
        match = re.fullmatch(r"TC-" + re.escape(tid) + r"-(\d{2,})", cid)
        audit.check(bool(match), "test_case_task_id", cid)
        if match:
            position = int(match.group(1)) - 1
            audit.check(0 <= position < len(task["tests"]), "test_case_position", cid)
            if 0 <= position < len(task["tests"]):
                audit.check(case.get("scenario") == task["tests"][position], "test_case_scenario_matches_task", cid)
        audit.check(set(case.get("operations", [])) == set(task["operations"]), "test_case_operations_match_task", cid)
        audit.check(set(case.get("tables", [])) == set(task["tables"]), "test_case_tables_match_task", cid)
        for name in case.get("schemas", []):
            audit.check(name in schemas, "test_schema_exists", {"id": cid, "schema": name})
        for oid in case.get("operations", []):
            audit.check(oid in operations, "test_operation_exists", {"id": cid, "operation": oid})
            if oid in by_operation:
                for field in ["request", "response"]:
                    schema_name = by_operation[oid].get(field)
                    if schema_name in schemas:
                        audit.check(schema_name in case.get("schemas", []), "test_schema_operation_trace", {"id": cid, "operation": oid, "field": field, "schema": schema_name})
        for oid in case.get("responseRepresentations", {}):
            audit.check(oid in case.get("operations", []), "test_response_operation", {"id": cid, "operation": oid})
        for field in ["apiContract", "databaseContract"]:
            audit.check(isinstance(case.get(field), str) and (directory / case[field]).is_file(), "test_contract_file", {"id": cid, "field": field, "path": case.get(field)})
        for field in ["scenario", "expected", "status"]:
            audit.check(isinstance(case.get(field), str) and bool(case[field].strip()), "test_nonempty_field", {"id": cid, "field": field})

    # Non-HTTP runtime/offline contracts must have the same task/schema trace.
    for extension_name, extension_value in spec.items():
        if not extension_name.startswith("x-"):
            continue
        for node in walk(extension_value):
            if not isinstance(node, dict) or not isinstance(node.get("task"), str):
                continue
            tid = node["task"]
            audit.check(tid in by_task, "extension_contract_task", {"extension": extension_name, "task": tid})
            declared_schemas = schema_refs(node)
            traced_schemas = set().union(*(set(case.get("schemas", [])) for case in cases if case.get("taskId") == tid))
            audit.check(declared_schemas <= traced_schemas, "extension_contract_test_trace", {"extension": extension_name, "task": tid, "missing": sorted(declared_schemas - traced_schemas)})

    # Dedicated CLI mapping table; prose mentions are not command exposure.
    cli_rows = table_rows(docs.get("CLI.md", ""), r"`[A-Za-z][A-Za-z0-9]*`\s*/\s*[^|]+")
    cli_ids = [re.match(r"`([^`]+)`", cells[0]).group(1) for _, cells in cli_rows]
    internal = {oid for oid, op in operations.items() if op.get("x-audience") == "internal-service"}
    public = set(operations) - internal
    counts.update({"cli_mapping_rows": len(cli_ids), "public_operations": len(public), "internal_operations": len(internal)})
    audit.check(len(internal) == args.expected_internal, "internal_count", {"expected": args.expected_internal, "actual": len(internal)})
    audit.check(not duplicate_values(cli_ids), "cli_mapping_unique", duplicate_values(cli_ids))
    audit.check(set(cli_ids) == public, "cli_public_operation_coverage", {"missing": sorted(public - set(cli_ids)), "extra": sorted(set(cli_ids) - public)})
    audit.check(not (set(cli_ids) & internal), "cli_internal_exclusion", sorted(set(cli_ids) & internal))
    for oid in internal:
        op = operations[oid]
        audit.check(op.get("x-client-surfaces") == ["service"], "internal_service_client_only", {"operation": oid, "clients": op.get("x-client-surfaces")})
    for lineno, cells in cli_rows:
        oid = re.match(r"`([^`]+)`", cells[0]).group(1)
        if oid in operations and len(cells) >= 5:
            op = operations[oid]
            audit.check(f"{op['method']} {op['path']}" in cells[1], "cli_http_mapping", {"line": lineno, "operation": oid})
            audit.check(op.get("x-permission", "") in cells[4] and op.get("x-release-task", "") in cells[4], "cli_permission_task_mapping", {"line": lineno, "operation": oid})

    # Old-route disposition and stable IDs; no interface count implies completion.
    audit.check(len(routes) == args.expected_routes, "legacy_route_count", {"expected": args.expected_routes, "actual": len(routes)})
    route_keys = [(r.get("method"), r.get("path")) for r in routes]
    audit.check(not duplicate_values(route_keys), "legacy_route_unique", duplicate_values(route_keys))
    for route in routes:
        key = f"{route.get('method')} {route.get('path')}"
        audit.check(route.get("classification") in CLASSIFICATIONS, "legacy_disposition", {"route": key, "classification": route.get("classification")})
        for field in ["handler", "source", "inventory", "primary_task"]:
            audit.check(isinstance(route.get(field), str) and bool(route[field].strip()), "legacy_required_field", {"route": key, "field": field})
        for tid in [route.get("primary_task")] + route.get("supporting_tasks", []):
            audit.check(tid in by_task, "legacy_stable_task_ref", {"route": key, "task": tid})
        for tid in re.findall(r"DM-[A-Z]+-\d{3}", route.get("task", "")):
            audit.check(tid in by_task, "legacy_display_task_ref", {"route": key, "task": tid})
    legacy_md_rows = table_rows(docs.get("LEGACY-ROUTES.md", ""), r"GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|WS")
    counts["legacy_route_table_rows"] = len(legacy_md_rows)
    audit.check(len(legacy_md_rows) == len(routes), "legacy_route_table_count", {"table": len(legacy_md_rows), "json": len(routes)})
    for lineno, cells in legacy_md_rows:
        if len(cells) >= 2:
            key = (cells[0], cells[1].strip("`"))
            audit.check(key in set(route_keys), "legacy_route_table_key", {"line": lineno, "route": key})

    # Frontend inventory keys are stable even when feature prose is corrected.
    fei_rows = table_rows(docs.get("FRONTEND-INVENTORY.md", ""), r"FEI-[A-Z]+\d{3}")
    fei_ids = [cells[0] for _, cells in fei_rows]
    counts["frontend_inventory_rows"] = len(fei_ids)
    audit.check(len(fei_ids) == args.expected_fei, "fei_count", {"expected": args.expected_fei, "actual": len(fei_ids)})
    audit.check(not duplicate_values(fei_ids), "fei_id_unique", duplicate_values(fei_ids))
    for lineno, cells in fei_rows:
        header_columns = table_header_columns(docs.get("FRONTEND-INVENTORY.md", ""), lineno)
        audit.check(len(cells) == header_columns, "fei_columns", {"line": lineno, "id": cells[0], "columns": len(cells), "header_columns": header_columns})
        for tid in re.findall(r"DM-[A-Z]+-\d{3}", "|".join(cells)):
            audit.check(tid in by_task, "fei_task_ref", {"id": cells[0], "task": tid})

    # Relative links are resolved from each actual markdown file, not cwd.
    link_pattern = re.compile(r"(?<!!)\[[^\]\n]*\]\(([^)\n]+)\)")
    link_count = 0
    for name, text in docs.items():
        for target in link_pattern.findall(text):
            target = target.strip().split(' "', 1)[0].strip("<>")
            parsed_url = urlsplit(target)
            if parsed_url.scheme or parsed_url.netloc or target.startswith("/"):
                continue
            path = unquote(parsed_url.path)
            if not path:
                continue
            link_count += 1
            audit.check((directory / path).exists(), "relative_doc_link", {"file": name, "target": target})
    counts["relative_doc_links_checked"] = link_count

    # Preserve pre-existing task, route, operation and feature identities.
    baseline = Path(args.baseline)
    if baseline.is_dir():
        for filename, current_set, keyfn in [
            ("tasks.json", set(by_task), lambda x: x["id"]),
            ("api-operations.json", set(operations), lambda x: x["id"]),
            ("legacy-routes.json", set(route_keys), lambda x: (x["method"], x["path"])),
            ("test-cases.json", {c["id"] for c in cases}, lambda x: x["id"]),
        ]:
            if (baseline / filename).exists():
                original = {keyfn(x) for x in json.loads((baseline / filename).read_text())}
                audit.check(original <= current_set, "baseline_ids_preserved", {"file": filename, "removed": sorted(original - current_set)})
        fei_baseline = baseline / "FRONTEND-INVENTORY.md"
        if fei_baseline.is_file():
            original_fei = {cells[0] for _, cells in table_rows(fei_baseline.read_text(), r"FEI-[A-Z]+\d{3}")}
            audit.check(original_fei <= set(fei_ids), "baseline_fei_ids_preserved", sorted(original_fei - set(fei_ids)))
    else:
        audit.warn("baseline_unavailable", str(baseline.resolve()))

    # Flag concurrent edits rather than claiming an atomic verification.
    changed_during_read = []
    for name, original_hash in fingerprints.items():
        current_hash = hashlib.sha256((directory / name).read_bytes()).hexdigest()
        if current_hash != original_hash:
            changed_during_read.append(name)
    audit.check(not changed_during_read, "inputs_stable_during_validation", changed_during_read)
    report = {
        "verified_at": datetime.now(timezone.utc).isoformat(),
        "directory": str(directory),
        "scope": "planning cross-reference validation only; no product tests or execution",
        "counts": counts,
        "checks": dict(sorted(audit.checks.items())),
        "errors": audit.errors,
        "warnings": audit.warnings,
        "fingerprints": fingerprints,
        "passed": not audit.errors,
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
