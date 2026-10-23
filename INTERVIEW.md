# Throttle interview walkthrough

## Two-minute pitch

Throttle is a Go HTTP admission gateway with a React traffic-control console. The hard part is deciding what a burst means while the upstream has finite capacity. I implemented token bucket, rolling-window log and GCRA, then a deliberately small causal policy based on request pressure, client-error EWMA and unclassified endpoint share. Pressure alone never cuts a budget. Configured API keys establish identity, rather than an attacker-controlled client-ID header.

The proof uses actual HTTP servers, not direct function calls pretending to be network traffic. Seeded offers go through the same authentication, limiter, capacity guard, circuit and reverse proxy as the live console. Every response and its timing is retained; fixed/adaptive conditions get separate instances, and order alternates across trials. The report includes healthy bursts and a valid but unclassified endpoint where the heuristic can hurt. Those controls matter more than an impressive isolated success percentage.

The implementation uses boring local locked state, a nonblocking origin-capacity semaphore and a fenced circuit breaker. Tests cover mathematical limiter envelopes, concurrent hot identities, spoofed headers, overload, real response timeout, half-open recovery and truncated-response cleanup under the race detector. The console shows real receipts and score components. The microbenchmark explicitly measures admission-core decisions, not total gateway latency. README.md and docs/BENCHMARKS.md are generated from raw outputs. There is no distributed quota, production adoption claim or completed Pulse integration.

## Ten-minute architecture walkthrough

Start with cmd/throttle/main.go. Demo mode explicitly starts a separate loopback synthetic origin and registers public fixture keys. Production-style single-instance mode instead requires a fixed trusted origin and out-of-band client-key JSON. External binding requires a private operator token. The process owns shutdown, deadlines and the optional synthetic campaign callback; it never accepts an arbitrary load target from the browser.

Trace /proxy through internal/gateway/server.go. Authenticate the configured key by digest, reject an oversized body before forwarding, derive endpoint class from the path and call Engine.Admit. A token permit does not mean success: origin capacity may be full or its circuit may be open. Those outcomes are separate counters/statuses. Only after those gates does the reverse proxy forward to the one configured origin, stripping caller credentials and forwarded identity. No automatic application replay is added.

Open limiter.go. Token bucket refills elapsed time using the previous rate before applying a new rate/cap. GCRA advances theoretical arrival time and preserves outstanding debt. The rolling log counts accepted timestamps in a trailing window, so its burst semantics differ from token/GCRA. All are protected by the owning engine lock; the tests make the accepted-envelope contract explicit rather than treating their behavior as interchangeable.

In engine.go, show feature buckets, the warm-up guard and score expression. Request pressure is not a learned probability or a complete burstiness model. Client-error feedback comes from completed origin responses, excluding server failures and quota responses. The request that produced the response cannot benefit from future feedback; the next decision sees the updated signal. A known healthy burst retains its base budget, but an unclassified healthy route can be penalized. Inspect the corresponding control and raw receipts before discussing improvement.

In breaker.go, explain the generation ticket and one half-open probe. A late successful request from before opening cannot close a later circuit. Caller cancellation can release a probe without training origin blame; a truncated response still finalizes the permit/capacity ownership even when ReverseProxy aborts the connection. The regression tests exercise actual body failure and timeout behavior.

Finish with internal/campaign and the React console. The load generator controls offered schedules, not successful outcomes. It validates full body consumption and reconciles every offer, rejecting invalid transport/load-client trials. Actual origin peak validates the capacity bound. The console's lifetime counters and last campaign result have different scopes and are labelled accordingly. The completion timeline leaves pre-start time unobserved. Show the favorable and unfavorable workloads and the exact overhead definition. All state is per process; restart and multiple replicas reset/multiply allowance.

## Five tradeoffs

- **Configured identities versus anonymous buckets:** key-derived identity resists header spoofing and bounds cardinality. It does not stop a principal with several keys from multiplying its share, and it has no dynamic provisioning/rotation system.
- **Local mutex versus shared admission:** ordinary locked state makes the concurrency argument inspectable and keeps dependencies small. It cannot enforce a cross-instance budget and may contend at scale. Redis mode is explicitly deferred.
- **Simple score versus inferred abuse:** explicit components explain decisions and make negative controls possible. Catalog gaps, warm-up behavior and a malicious client on healthy endpoints limit effectiveness. The score is not calibrated accuracy.
- **Immediate capacity rejection versus fair queuing:** a semaphore gives a hard origin bound and avoids unbounded queue latency. Arrival order can starve an otherwise allowed client. A fair queue needs its own latency/cancellation tests and measurements.
- **One trusted origin versus fallback/replay:** not retrying application requests avoids accidental duplicate effects. Recovery is a fenced probe, not failover or consensus. Supporting retries requires idempotency semantics, especially for POST.

## Five failure modes

- **Concurrent quota overshoot:** decision/state mutation is locked; a hot-identity test checks an immediate burst under concurrent callers, and randomized tests check accepted envelopes. Race detection is additional finite evidence, not a proof of fairness.
- **Spoofed identity or malformed request:** the configured key must authenticate; forwarded identity and caller secrets are stripped. Size/shape/origin/host validation rejects malformed traffic before it reaches the trusted backend.
- **Abuse crowds out legitimate work:** client admission plus adaptive reductions can reduce pressure on the origin, and the semaphore bounds gateway request leases. An origin that ignores cancellation may continue work after a gateway deadline. The measured comparisons expose remaining legitimate capacity losses instead of promising universal fairness.
- **Origin hangs or fails:** response-header/overall deadlines and a fenced circuit limit exposure. Half-open probes are serialized, stale completions are ignored, and server failures do not teach that clients are abusive.
- **Heuristic blames a healthy new route:** the unclassified fixture returns valid results but receives a lower adaptive budget. The test and benchmark retain this harm. Review the catalog or keep fixed mode before any real integration.

## Fifteen likely questions

1. **Where is an admission decision atomic?** Under Engine's mutex, across feature update, effective-budget computation and the client limiter transition. Upstream work occurs outside that lock.
2. **Why not trust X-Client-ID?** A caller could create fresh identities to reset its allowance or impersonate another account. The registry derives the label from a configured key; the label is never a public authority claim.
3. **What does “burstiness” mean here?** A clipped recent-attempt pressure ratio. It is a simple proxy signal, not a variance estimator or a learned fingerprint. The design names that limit explicitly.
4. **Why include rejected attempts in pressure?** Otherwise a caller could keep hammering the gateway while the behavior signal saw only admitted traffic. Those attempts still do not become client-error feedback.
5. **Why refill at the previous rate?** Refilling old elapsed time at a newly increased rate would mint retroactive credit. The token test exercises that transition boundary.
6. **How does GCRA admit its first batch?** Its tolerance is one fewer interval than the configured burst, so the first permit advances debt and the immediate batch is bounded. The next eligibility time becomes Retry-After evidence.
7. **Is a rolling log equivalent to token bucket?** No. The log is an exact trailing-window cap; token/GCRA allow rate-plus-burst behavior. The UI and benchmark table name the different semantics.
8. **Does a permit guarantee a successful response?** No. Global capacity, circuit, upstream response and network completion still matter. The report keeps rate denials and capacity/circuit denials distinct.
9. **Why not refund a permit after a capacity denial?** Refunds complicate atomicity and can let clients repeatedly compete without spending budget. This simple implementation documents the cost; a fair queue is future work.
10. **Can an outage make all clients look abusive?** Server/transport/circuit/capacity failures are excluded from client-error learning. Completed client-caused errors and unclassified route mix are separate signals.
11. **Can the heuristic hurt legitimate traffic?** Yes. A healthy endpoint outside the catalog can be penalized. The preserved unclassified workload measures this directly; it is not hidden behind an overall score.
12. **What does the circuit ticket solve?** Late completions from the previous generation cannot alter a newer open/half-open state. Only the admitted probe in the matching epoch can recover it.
13. **What happens if the origin body is truncated after headers?** ReverseProxy can abort the stream; deferred cleanup still releases capacity and finalizes the circuit. The client load generator validates body completion and rejects the trial as invalid instead of counting a false success.
14. **What does the ns/op figure include?** Admission lock, feature update and limiter decision with deterministic supplied timestamps. It excludes HTTP/auth/proxy/response feedback, and reports the actual admitted fraction of this mostly-denied workload. It is not end-to-end overhead.
15. **Why not put this directly in front of Pulse?** Pulse owns a long-lived authenticated voice WebSocket and separate worker credentials. The current HTTP deadlines/credential stripping are not a turn-admission adapter. A narrow public admission package and separately tested integration are proposed, not implemented.

## What I would do with three more months

Collect an independent synthetic/allowed-public workload and evaluate per-client success and false penalties, not just total throughput. Make endpoint classification reviewable and measure fair queuing with cancellation/latency budgets. Design explicit key provisioning and global quota semantics before implementing shared-state admission; test network partitions and process replacement rather than assuming Redis solves them. Add sustained contention and allocation profiling, strengthen parser/HTTP fuzzing, and evaluate long-lived turn admission as a separately approved Pulse change. Preserve the old comparison controls so a more complex policy must demonstrate what it improves and what it harms.
