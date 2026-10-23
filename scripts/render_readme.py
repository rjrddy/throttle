"""Build measured reports only from raw command outputs and response receipts."""

from __future__ import annotations

import json
import re
import statistics
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    data = json.loads((ROOT / "docs/results/campaign/summary.json").read_text())
    tests = json.loads((ROOT / "docs/results/tests.json").read_text())
    groups: dict[tuple[str, str, str], list[dict]] = {}
    for run in data["runs"]:
        raw = ROOT / "docs/results/campaign" / run["raw_file"]
        receipt = json.loads(raw.read_text())
        assert len(receipt["receipts"]) == sum(c["offered"] for c in run["classes"].values())
        assert all(r["status"] and r["outcome"] != "body-error" for r in receipt["receipts"])
        assert receipt["peak_origin_inflight"] <= data["origin_capacity"]
        groups.setdefault((run["scenario"], run["algorithm"], run["mode"]), []).append(run)
    aggregates = []
    for (scenario, algorithm, mode), runs in groups.items():
        good = [r["classes"]["legitimate"] for r in runs]
        offered = sum(c["offered"] for c in good)
        success = sum(c["success"] for c in good)
        aggregates.append(
            {
                "scenario": scenario,
                "algorithm": algorithm,
                "mode": mode,
                "trials": len(runs),
                "offered": offered,
                "success": success,
                "success_rate": success / offered,
                "limited": sum(c["limited"] for c in good),
                "capacity": sum(c["capacity"] for c in good),
                "other": sum(c["other"] for c in good),
                "median_run_p99_ms": statistics.median(c["p99_ms"] for c in good),
                "min_trial_success_rate": min(c["success_rate"] for c in good),
                "max_trial_success_rate": max(c["success_rate"] for c in good),
                "origin_requests": sum(r["origin_requests"] for r in runs),
                "peak_origin_inflight": max(r["peak_origin_inflight"] for r in runs),
            }
        )
    samples: dict[str, list[dict]] = {}
    for line in (ROOT / "docs/results/admission-bench.txt").read_text().splitlines():
        if not line.startswith("BenchmarkAdmission/"):
            continue
        name = line.split()[0].removeprefix("BenchmarkAdmission/").rsplit("-", 1)[0]
        metrics = {}
        for unit in ["ns/op", "B/op", "allocs/op", "allowed_pct"]:
            match = re.search(r"([\d.]+)\s+" + re.escape(unit), line)
            if not match:
                raise ValueError("missing benchmark metric: " + unit)
            metrics[unit] = float(match[1])
        samples.setdefault(name, []).append(metrics)
    overhead = [
        {
            "case": name,
            "samples": len(rows),
            "median_ns_op": statistics.median(r["ns/op"] for r in rows),
            "min_ns_op": min(r["ns/op"] for r in rows),
            "max_ns_op": max(r["ns/op"] for r in rows),
            "median_bytes_op": statistics.median(r["B/op"] for r in rows),
            "median_allocs_op": statistics.median(r["allocs/op"] for r in rows),
            "median_allowed_pct": statistics.median(r["allowed_pct"] for r in rows),
        }
        for name, rows in samples.items()
    ]
    result = {"campaigns": aggregates, "admission_overhead": overhead}
    (ROOT / "docs/results/report.json").write_text(json.dumps(result, indent=2) + "\n")

    def condition(scenario: str, algorithm: str, mode: str) -> dict:
        return next(
            r
            for r in aggregates
            if (r["scenario"], r["algorithm"], r["mode"]) == (scenario, algorithm, mode)
        )

    fixed = condition("mixed", "token", "fixed")
    adaptive = condition("mixed", "token", "adaptive")
    harmful_fixed = condition("unclassified", "token", "fixed")
    harmful_adaptive = condition("unclassified", "token", "adaptive")
    cost = next(r for r in overhead if r["case"] == "adaptive/token//v1/read")
    text = f"""# Throttle

## Problem

An API gateway needs to preserve useful traffic under abuse without treating every legitimate burst as an attack.

## Results

- Mixed synthetic token-bucket campaign: legitimate HTTP success **{fixed["success_rate"]:.1%} fixed → {adaptive["success_rate"]:.1%} adaptive**, **{adaptive["offered"]:,} offers per mode** across **{adaptive["trials"]} trials**; observed peak origin concurrency **{adaptive["peak_origin_inflight"]}**, matching the configured cap.
- Admission-core adaptive/token/read decision: **{cost["median_ns_op"]:.1f} ns/op** median of **{cost["samples"]} Go benchmark samples**, **{cost["median_allocs_op"]:g} allocs/op**; this saturated workload admitted **{cost["median_allowed_pct"]:.2f}%** of decisions and excludes HTTP/auth/response feedback.
- Harmful control, healthy unclassified endpoint: **{harmful_fixed["success_rate"]:.1%} fixed → {harmful_adaptive["success_rate"]:.1%} adaptive** success; **{tests["passed_tests_including_subtests"]} passing test/subtest events** under the race detector. The heuristic does hurt legitimate traffic in this case.

[Every raw response receipt](docs/results/campaign/), [Go microbenchmark output](docs/results/admission-bench.txt), [generated report](docs/results/report.json), [raw race-test events](docs/results/tests.jsonl). Campaigns use a real Go HTTP origin and reverse proxy, a seeded offered schedule, a synthetic service delay and a finite concurrency cap. Each condition gets a fresh instance, with fixed/adaptive order alternating by trial. Outcomes and latency are measured, not deterministic. See [all algorithms, controls and trial ranges](docs/BENCHMARKS.md) and [actual host/toolchain metadata](docs/results/platform.json). Short shared-laptop tests do not establish production capacity or real-world abuse-detection accuracy.

Caught failures: actual rate denial, capacity overload, body-size rejection, origin timeout and truncated-body abort appear in [fault receipts](docs/results/fault-receipts.json). Reproduce with `make test`. The truncated-response regression checks that a half-open probe and capacity lease cannot remain stuck. [Quality report](docs/QUALITY.md) · [CI](https://github.com/rjrddy/throttle/actions).

## Architecture

```mermaid
flowchart LR
    client[Configured API-key client] --> identity[Authenticate / remove untrusted identity headers]
    identity --> signals[Pressure / client-error EWMA / endpoint mix]
    signals --> limiter[Token bucket / rolling window / GCRA]
    limiter --> capacity[Bounded origin concurrency]
    capacity --> breaker[Fenced circuit / one recovery probe]
    breaker --> origin[One trusted HTTP origin]
    origin --> feedback[Client errors separate from server faults]
    feedback --> signals
    console[React traffic-control console] --> admin[Same-origin operator API]
    admin --> signals
    admin --> receipts[Actual outcomes / bounded history]
```

![Actual live React console](docs/results/browser/desktop.png)

## Run it

Prerequisites: Go with toolchain-download support, Node, Python and Make. Setup installs pinned developer tools and the locked React dependencies. The demo starts the actual gateway and its own loopback synthetic origin. Open the printed URL; Ctrl-C stops both.

```sh
make demo
# Open http://127.0.0.1:8086; use another terminal for the request below.
curl -H 'Authorization: Bearer synthetic-throttle-amber-key-0001' http://127.0.0.1:8086/proxy/v1/read
```

The key above is a public synthetic fixture. `make lint test scan` runs local gates; `make bench` saves microbenchmarks and actual HTTP comparisons, then rebuilds this Results section from them. [Ninety-second demo steps](docs/DEMO.md), [browser proof](docs/results/browser/verification.json), [protocol](docs/PROTOCOL.md).

## Decisions and limits

Use ordinary locked local state, configured client identities and explicit score components. Pressure alone never reduces a budget; client-caused errors and unclassified endpoint mix can. The endpoint catalog is deliberately tiny, making false positives visible. The concurrency guard sheds work instead of providing a fair queue; arrival order can still hurt an individual client. Rolling window uses a trailing-second cap and differs from token/GCRA burst semantics. [Design](docs/DESIGN.md) and [ADRs](docs/adr/) explain the guarantees.

All budgets/signals/counters are per process and reset on replacement. Redis/distributed mode is optional and unimplemented. No arbitrary client-ID trust, machine-learning claim, provider adoption claim, automatic application retry or complete production security perimeter. The microbenchmark is admission-core overhead, not end-to-end gateway overhead. Client round-trip p99s in the full table include body consumption. Multiple API keys can multiply a principal's allowance; there is no Sybil protection or global cross-instance quota.

Not deployed. [Render free-plan template](render.yaml) is prepared only and does not prove hosted capacity/availability. The image defaults to synthetic demo mode; [hosting details](docs/DEPLOY.md). [Migration provenance](docs/migration/throttle.md), [interview walkthrough](INTERVIEW.md), [follow-up drill](DRILL.md). Pulse is unchanged; its long-lived voice/WebSocket path needs a separately approved [integration adapter](docs/PULSE_INTEGRATION_PROPOSAL.md).

## Next

Review endpoint catalogs against an independent workload, measure a fair origin queue and shared-state admission, then design global quotas and restart behavior. Evaluate a separately approved Pulse turn-admission adapter without forwarding voice streams through the current bounded-request proxy.
"""
    (ROOT / "README.md").write_text(text)
    table = "# Measured comparisons\n\nGenerated from raw actual-HTTP response receipts. Success is successful complete HTTP responses / offered requests, pooled across the stated trials. p99 is the median of per-trial p99s, not a pooled percentile.\n\n| Scenario | Algorithm | Mode | Legitimate success | Offers | Rate denials | Capacity/circuit denials | Other | Trial success range | Median run p99 ms | Peak origin inflight |\n|---|---|---|---:|---:|---:|---:|---:|---|---:|---:|\n"
    for r in aggregates:
        table += f"| {r['scenario']} | {r['algorithm']} | {r['mode']} | {r['success_rate']:.1%} | {r['offered']} | {r['limited']} | {r['capacity']} | {r['other']} | {r['min_trial_success_rate']:.1%}–{r['max_trial_success_rate']:.1%} | {r['median_run_p99_ms']:.3f} | {r['peak_origin_inflight']} |\n"
    table += "\n## Admission-core microbenchmark\n\nMedian across individual Go benchmark samples; range is min–max ns/op. Deterministic supplied timestamps advance at a fixed offered cadence after signal warm-up. Reported acceptance fraction exposes the predominantly denied-path workload. Excludes network, key hashing, proxy body handling and response-feedback bookkeeping. It is not HTTP throughput or total added gateway latency.\n\n| Case | Samples | Median ns/op | Range ns/op | Bytes/op | Allocs/op | Admitted decisions |\n|---|---:|---:|---|---:|---:|---:|\n"
    for r in overhead:
        table += f"| {r['case'].replace('//v1/', '/')} | {r['samples']} | {r['median_ns_op']:.1f} | {r['min_ns_op']:.1f}–{r['max_ns_op']:.1f} | {r['median_bytes_op']:g} | {r['median_allocs_op']:g} | {r['median_allowed_pct']:.2f}% |\n"
    table += """
## Interpretation and reproduction

Run make setup, then make bench. Every HTTP scenario uses actual loopback servers and one new gateway/origin pair per mode/algorithm/trial. Mixed sends legitimate bursts alongside repeated unknown-endpoint errors. Healthy-burst removes abuse. Unclassified sends a healthy client to the fixture's valid preview route, which is intentionally absent from the admission catalog: adaptation can reject useful work. Modes share identical offered plans, base budgets and origin capacity. No client prefix is consulted by the production policy; labels identify synthetic cohorts only for analysis.

Global capacity rejection is not a per-client rate denial. There is no fair queue: a client's arrival phase can change success even when its budget stays intact. Read individual client receipts before asserting fairness. A higher overall success rate does not prove every client's success improved. The synthetic origin delay bounds useful capacity; real origins and endpoint distributions need independent evaluation. The score is not a calibrated probability. No thresholds or requests were selected after observing outcomes to remove the unfavorable case.

Pilot results are retained separately and excluded from this table. Final workload parameters, toolchain, machine specs and command logs are saved. CPU affinity/dedicated-host isolation are not provided. Exact outcomes vary with OS scheduling. Go tests use deterministic clocks/seeds for mathematical limit invariants; real HTTP timing does not. CI runs race/property/integration and short HTTP reconciliation, excluding the full long comparison suite.

Sources informing the implementation, without imported code/assets: [Go ReverseProxy](https://go.dev/pkg/net/http/httputil/) and [Redis rate-limiter algorithm overview](https://redis.io/tutorials/howtos/ratelimiting/). The local contracts and finite guarantees are in docs/DESIGN.md and tested explicitly.
"""
    (ROOT / "docs/BENCHMARKS.md").write_text(table)
    raw_events = [
        json.loads(line) for line in (ROOT / "docs/results/tests.jsonl").read_text().splitlines()
    ]
    faults = [event for event in raw_events if "caught " in event.get("Output", "")]
    (ROOT / "docs/results/fault-receipts.json").write_text(json.dumps(faults, indent=2) + "\n")


if __name__ == "__main__":
    main()
