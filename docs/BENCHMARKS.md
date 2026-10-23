# Measured comparisons

Generated from raw actual-HTTP response receipts. Success is successful complete HTTP responses / offered requests, pooled across the stated trials. p99 is the median of per-trial p99s, not a pooled percentile.

| Scenario | Algorithm | Mode | Legitimate success | Offers | Rate denials | Capacity/circuit denials | Other | Trial success range | Median run p99 ms | Peak origin inflight |
|---|---|---|---:|---:|---:|---:|---:|---|---:|---:|
| mixed | token | fixed | 52.8% | 492 | 0 | 232 | 0 | 46.3%–57.9% | 62.542 | 4 |
| mixed | token | adaptive | 68.5% | 492 | 0 | 155 | 0 | 67.1%–69.5% | 62.002 | 4 |
| mixed | sliding | fixed | 58.5% | 492 | 0 | 204 | 0 | 58.5%–58.5% | 62.555 | 4 |
| mixed | sliding | adaptive | 71.1% | 492 | 0 | 142 | 0 | 70.7%–71.3% | 62.089 | 4 |
| mixed | gcra | fixed | 59.3% | 492 | 0 | 200 | 0 | 57.3%–61.0% | 62.209 | 4 |
| mixed | gcra | adaptive | 69.7% | 492 | 0 | 149 | 0 | 69.5%–70.1% | 61.843 | 4 |
| healthy-burst | token | fixed | 60.0% | 420 | 0 | 168 | 0 | 60.0%–60.0% | 63.489 | 4 |
| healthy-burst | token | adaptive | 60.0% | 420 | 0 | 168 | 0 | 60.0%–60.0% | 63.346 | 4 |
| unclassified | token | fixed | 91.0% | 300 | 27 | 0 | 0 | 91.0%–91.0% | 63.425 | 2 |
| unclassified | token | adaptive | 67.0% | 300 | 99 | 0 | 0 | 67.0%–67.0% | 63.423 | 2 |

## Admission-core microbenchmark

Median across individual Go benchmark samples; range is min–max ns/op. Deterministic supplied timestamps advance at a fixed offered cadence after signal warm-up. Reported acceptance fraction exposes the predominantly denied-path workload. Excludes network, key hashing, proxy body handling and response-feedback bookkeeping. It is not HTTP throughput or total added gateway latency.

| Case | Samples | Median ns/op | Range ns/op | Bytes/op | Allocs/op | Admitted decisions |
|---|---:|---:|---|---:|---:|---:|
| fixed/token/read | 5 | 82.9 | 82.6–83.0 | 0 | 0 | 2.00% |
| fixed/token/missing | 5 | 87.3 | 87.3–87.6 | 0 | 0 | 2.00% |
| fixed/sliding/read | 5 | 78.6 | 78.3–80.7 | 0 | 0 | 2.00% |
| fixed/sliding/missing | 5 | 79.5 | 79.1–81.7 | 0 | 0 | 2.00% |
| fixed/gcra/read | 5 | 86.6 | 86.2–87.0 | 0 | 0 | 2.00% |
| fixed/gcra/missing | 5 | 87.0 | 86.4–87.5 | 0 | 0 | 2.00% |
| adaptive/token/read | 5 | 84.0 | 83.6–84.3 | 0 | 0 | 2.00% |
| adaptive/token/missing | 5 | 99.0 | 98.6–99.3 | 0 | 0 | 0.20% |
| adaptive/sliding/read | 5 | 79.8 | 78.3–87.3 | 0 | 0 | 2.00% |
| adaptive/sliding/missing | 5 | 91.3 | 90.5–92.9 | 0 | 0 | 0.30% |
| adaptive/gcra/read | 5 | 87.0 | 86.6–87.8 | 0 | 0 | 2.00% |
| adaptive/gcra/missing | 5 | 98.0 | 97.2–105.1 | 0 | 0 | 0.20% |

## Interpretation and reproduction

Run make setup, then make bench. Every HTTP scenario uses actual loopback servers and one new gateway/origin pair per mode/algorithm/trial. Mixed sends legitimate bursts alongside repeated unknown-endpoint errors. Healthy-burst removes abuse. Unclassified sends a healthy client to the fixture's valid preview route, which is intentionally absent from the admission catalog: adaptation can reject useful work. Modes share identical offered plans, base budgets and origin capacity. No client prefix is consulted by the production policy; labels identify synthetic cohorts only for analysis.

Global capacity rejection is not a per-client rate denial. There is no fair queue: a client's arrival phase can change success even when its budget stays intact. Read individual client receipts before asserting fairness. A higher overall success rate does not prove every client's success improved. The synthetic origin delay bounds useful capacity; real origins and endpoint distributions need independent evaluation. The score is not a calibrated probability. No thresholds or requests were selected after observing outcomes to remove the unfavorable case.

Pilot results are retained separately and excluded from this table. Final workload parameters, toolchain, machine specs and command logs are saved. CPU affinity/dedicated-host isolation are not provided. Exact outcomes vary with OS scheduling. Go tests use deterministic clocks/seeds for mathematical limit invariants; real HTTP timing does not. CI runs race/property/integration and short HTTP reconciliation, excluding the full long comparison suite.

Sources informing the implementation, without imported code/assets: [Go ReverseProxy](https://go.dev/pkg/net/http/httputil/) and [Redis rate-limiter algorithm overview](https://redis.io/tutorials/howtos/ratelimiting/). The local contracts and finite guarantees are in docs/DESIGN.md and tested explicitly.
