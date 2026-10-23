# Throttle design

## Why the hard part is hard

A fixed per-client cap does not describe a client's behavior, and reducing every burst penalizes ordinary interactive use. Admission also competes for a finite upstream: an accepted request can consume origin time and make another client's request fail. Throttle separates identity, behavioral signals, per-client admission, an origin-concurrency bound and origin-health protection. The benchmark measures their combined effect instead of equating limiter acceptance with successful work.

## Identity and threat model

Configured API keys map to a bounded registry of client labels. The request cannot choose its own identity through X-Client-ID or forwarded headers. Raw keys are hashed for lookup and never appear in state/metrics/events. The proxy strips authorization/API-key/forwarded identity headers before sending to its one configured trusted origin. There is no runtime account registration or arbitrary-URL load generator. Multiple keys still multiply a principal's budget: API-key authentication is not a solution to Sybil attacks, shared credentials or unfair account provisioning.

Default listening is loopback, with loopback Host validation and same-origin checks for browser access. External binding requires an environment admin token. Operator routes require that token outside local mode; client routes require a configured client key. The native server has no TLS; HTTPS belongs at a reviewed reverse proxy. Request bodies are buffered with an explicit limit before forwarding. Logs record lifecycle/policy facts, not body contents, raw URLs, tokens or arbitrary client labels.

## Limiter definitions

Token bucket refills at the previous rate for elapsed time, clamps to the new burst capacity, then consumes a token. This avoids minting retroactive credit when adaptation changes the configured rate. GCRA stores theoretical arrival time; tolerance is (burst − 1) intervals, so the initial immediate batch is exactly burst. A rate change changes future spacing without resetting outstanding debt. The rolling-window log admits at most ceil(effective rate) requests in any trailing second; its burst input is not used. This semantic difference is explicit in the UI/protocol docs and benchmark settings. Each client's limiter, feature state and counters are protected by the engine mutex. There is no distributed/global rate guarantee.

An explicit operator policy edit resets budgets and behavioral signals, increments a version and keeps lifetime outcome counters. That is an operational reset and can admit a fresh burst. In-flight responses can subsequently update signals; the UI is observational across policy edits. Benchmarks use a fresh instance per condition, never mix modes in one reported trial, and block live synthetic-job policy edits until that job finishes.

## Explainable adaptation

The implementation uses rolling request counts in short time buckets, response-error EWMA and the share of requests outside a tiny reviewed endpoint catalog. Request pressure is a clipped ratio of recent attempts to base rate, not a statistical inter-arrival model. Score weights, warm-up threshold and budget formula are ordinary constants in engine.go. The important causal guard is that pressure alone never cuts the budget. A client-error or unclassified-endpoint signal must also be present after warm-up. Returned rate denials, capacity denials, circuit denials and origin server failures never train client-error EWMA. Only completed origin responses below server-error status update it, excluding origin quota responses from client-error blame.

This is a heuristic, not an abuse classifier with a calibrated probability. A healthy new endpoint can receive an unjustified penalty; the unclassified workload deliberately demonstrates that case. An adversary making only healthy requests receives the ordinary fixed budget. Feature windows age out; successful responses decay error EWMA. There is no permanent ban, demographic input, inferred identity or machine-learning claim. Fixed and adaptive modes share the same base rate, origin capacity, workload plan and implementation; only the documented budget multiplier changes.

## Origin bound and fenced circuit

After client admission, a nonblocking semaphore bounds outstanding forwarded-request leases. A full semaphore rejects promptly instead of queuing. A consumed client permit is not refunded on capacity/fault rejection. This is deliberately simple; it does not guarantee fair queuing or protect an individual client from arrival-order effects. Response-header and overall deadlines bound gateway waiting. They cannot stop work in an origin that ignores request cancellation; the synthetic fixture cooperates with cancellation. The measured origin peak is evidence for that fixture, not a universal downstream CPU/work bound.

The breaker counts origin server/transport failures, opens, then allows only one half-open probe. A generation ticket fences late completions: a request admitted before opening cannot close a later circuit or interfere with its probe. Caller cancellation can release a probe without blaming the origin. ReverseProxy can abort a truncated response with ErrAbortHandler; deferred finalization still releases capacity and records the failed stream rather than leaving a probe stuck. Tests cover these paths. No application-level retries/failover are added; replaying a forwarded POST could duplicate effects. A configured trusted origin is the only destination.

## Observation and live console

The React console polls actual engine state and renders registered clients, scores/components, budgets, a bounded completion-event ring and a short rolling completion timeline. The timeline includes only the observed process lifetime, leaving earlier time unobserved. Counts are lifetime counters unless a completed campaign explicitly scopes them. The live synthetic campaign uses the same proxy routes/API keys as external requests; it is enabled only in demo mode. Its result can overlap operator requests and is illustrative. The isolated benchmark creates fresh actual loopback origin and gateway HTTP servers for every condition, with no direct-call substitute.

The UI uses original blue/white wayfinding graphics and system fonts, informed by [TfL's line-diagram design standard](https://content.tfl.gov.uk/tfl-line-diagram-standard.pdf). No roundel, route branding, licensed typeface, assets or source code is imported. The routing diagram is explanatory and static; the telemetry and receipts are real.

## What the proof establishes

Fake-clock unit/property tests check limiter boundaries, changing-rate credit, rolling-window and burst envelopes, and a deliberately invalid admission control. Concurrent hot-identity tests check that one immediate batch cannot overshoot. Actual HTTP tests cover identity spoofing, input limits, same-origin controls, overload, response timeout/circuit recovery and truncated-body cleanup under the race detector. These finite tests are evidence, not an exhaustive proof of fairness or every network schedule.

Campaigns replay seeded offered schedules, preserve every response/status/timestamp, alternate fixed/adaptive order by trial, and reject invalid load-generator trials. Counts reconcile offered = success + rate denial + capacity/circuit denial + other. Synthetic origin service time and concurrency are controlled, but OS scheduling, latency and outcomes are measured, not seeded. The origin peak validates the capacity bound. Healthy burst traffic and a healthy unclassified endpoint provide controls, including a case where adaptation hurts. All numbers come from retained receipts; do not extrapolate to real fraud detection or production capacity.

The admission microbenchmark includes the engine lock, feature update and limiter decision, with deterministic supplied timestamps and a described warm-up. It excludes network, auth hashing, response feedback and proxying; it is not total gateway overhead. End-to-end campaign p99s measure actual offered request round trips including body consumption. Short trials and shared-laptop scheduling limit statistical conclusions.
