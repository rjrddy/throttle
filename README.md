# Throttle

## Problem

An API gateway needs to preserve useful traffic under abuse without treating every legitimate burst as an attack.

## Results

- Mixed synthetic token-bucket campaign: legitimate HTTP success **52.8% fixed → 68.5% adaptive**, **492 offers per mode** across **3 trials**; observed peak origin concurrency **4**, matching the configured cap.
- Admission-core adaptive/token/read decision: **84.0 ns/op** median of **5 Go benchmark samples**, **0 allocs/op**; this saturated workload admitted **2.00%** of decisions and excludes HTTP/auth/response feedback.
- Harmful control, healthy unclassified endpoint: **91.0% fixed → 67.0% adaptive** success; **24 passing test/subtest events** under the race detector. The heuristic does hurt legitimate traffic in this case.

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

Use ordinary locked local state, configured client identities and explicit score components. Pressure alone never reduces a budget; client-caused errors and unclassified endpoint mix can. The endpoint catalog is deliberately tiny, making false positives visible. Cancellation bounds gateway waiting, not work in a noncooperating origin. The concurrency guard sheds work instead of providing a fair queue; arrival order can still hurt an individual client. Rolling window uses a trailing-second cap and differs from token/GCRA burst semantics. [Design](docs/DESIGN.md) and [ADRs](docs/adr/) explain the guarantees.

All budgets/signals/counters are per process and reset on replacement. Redis/distributed mode is optional and unimplemented. No arbitrary client-ID trust, machine-learning claim, provider adoption claim, automatic application retry or complete production security perimeter. The microbenchmark is admission-core overhead, not end-to-end gateway overhead. Client round-trip p99s in the full table include body consumption. Multiple API keys can multiply a principal's allowance; there is no Sybil protection or global cross-instance quota.

Not deployed. [Render free-plan template](render.yaml) is prepared only and does not prove hosted capacity/availability. The image defaults to synthetic demo mode; [hosting details](docs/DEPLOY.md). [Migration provenance](docs/migration/throttle.md), [interview walkthrough](INTERVIEW.md), [follow-up drill](DRILL.md). Pulse is unchanged; its long-lived voice/WebSocket path needs a separately approved [integration adapter](docs/PULSE_INTEGRATION_PROPOSAL.md).

## Next

Review endpoint catalogs against an independent workload, measure a fair origin queue and shared-state admission, then design global quotas and restart behavior. Evaluate a separately approved Pulse turn-admission adapter without forwarding voice streams through the current bounded-request proxy.
