# HTTP contract and bounds

GET /health is a public gateway liveness response. GET / serves the built React console. Versioned Vite assets are served only from /assets/. Loopback deployment validates Host as localhost or the loopback IPv4 address; browser Origin must match the HTTP/HTTPS request host.

Client requests use GET or POST /proxy/<origin path> with `Authorization: Bearer <configured API key>`. The prefix is removed, the original query is forwarded within a bounded length, and the destination is the explicitly configured trusted origin. Caller Authorization, X-API-Key, X-Client-ID and forwarded identity headers are stripped. No idempotency/retry/failover guarantee is added. A trusted origin must implement its own application authorization if needed; key authentication at this gateway is not delegation of upstream permissions.

Responses: invalid/missing key → 401; oversize body → 413 before forwarding; per-client admission denied → 429 with Retry-After; full origin capacity or open circuit → 503; transport/response-header deadline failure → 502; otherwise origin status/body is forwarded. Interrupted/truncated streaming responses can abort the connection after headers, so a logged failure decision is not a promise that its status reached the peer. The load generator checks full response-body consumption and rejects such an experiment as invalid.

Operator routes use THROTTLE_ADMIN_TOKEN outside local-only mode:

- GET /api/state: actual policy/version, lifetime client counters/signals, bounded completion timeline/receipts, current inflight/circuit, synthetic-job state.
- PUT /api/policy: strict JSON mode, algorithm, rate, burst. Unknown/trailing fields are rejected; live synthetic jobs block edits.
- GET /metrics: Prometheus-format fixed-label client outcome counters.
- POST /api/campaign: demo mode only; start the seeded real-HTTP synthetic workload; a simultaneous start is rejected.
- PUT /api/origin: demo mode only; healthy/fail/slow synthetic origin. Live synthetic jobs block edits.

Code configuration bounds: at most 128 registered client labels, label length 32, API key length 24–256; base rate 1–1000/s and burst 1–1000; concurrency cap 1–64 (demo uses 4); operator JSON 4096 bytes; body 65536 bytes; path 256 bytes/query 2048 bytes; server headers 8192 bytes. These are configured limits, not measured performance. Rolling-window burst semantics differ: ceil(effective rate) in a trailing second; the burst field applies to token/GCRA only.

All state is local memory and resets when a process is replaced. No Redis/distributed mode, identity federation, dynamic tenant provisioning, global quota, long-lived WebSocket turn accounting or hosted capacity claim. Synthetic demo keys are public fixture strings, never real credentials. Supply real deployment secrets out of band; no .env is committed.
