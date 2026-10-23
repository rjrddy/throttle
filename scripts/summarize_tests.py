"""Summarize actual Go test events, retaining every raw event."""

import json
from pathlib import Path

rows = [json.loads(line) for line in Path("docs/results/tests.jsonl").read_text().splitlines()]
passed = [r for r in rows if r.get("Action") == "pass" and r.get("Test")]
failed = [r for r in rows if r.get("Action") == "fail"]
result = {
    "passed_tests_including_subtests": len(passed),
    "failed": failed,
    "tests": [r["Package"] + "/" + r["Test"] for r in passed],
}
Path("docs/results/tests.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
if failed:
    raise SystemExit(1)
