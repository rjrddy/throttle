# Throttle follow-up drill

1. **Can a high-pressure healthy client lose allowance solely from pressure?** No. The adaptive guard additionally requires client-error or unclassified-route evidence after warm-up. Capacity can still reject it independently.
2. **Can the same principal obtain more share by using more keys?** Yes. Keys are a configured identity boundary, not Sybil resistance. Provisioning/fair-share policy is out of scope.
3. **Can a request spend credit from the future after a rate change?** Token refill uses the previous rate for old elapsed time. GCRA preserves prior debt instead of resetting its theoretical arrival time.
4. **Do the limiters have identical burst behavior?** No. Token/GCRA have explicit burst tolerance; the rolling log limits a trailing-second count. Cross-algorithm results must preserve those definitions.
5. **Why include a warm-up threshold?** Very little evidence should not immediately imply abuse. It also creates a low-volume evasion window; no complete abuse-detection guarantee is claimed.
6. **Does client-error EWMA age out with no traffic?** It decays with completed healthy responses, not wall time alone. The recent endpoint/pressure buckets age out; the distinction is explicit in the design.
7. **What if a client only hammers a healthy endpoint?** It receives its ordinary fixed budget, rather than an inferred malicious classification. Origin capacity remains bounded, but fairness can still suffer.
8. **Can an adaptive policy reduce budget for a valid new route?** Yes. The valid preview-route control demonstrates that exact false penalty. Review the endpoint catalog or use fixed mode.
9. **Could a capacity denial become a 4xx training sample?** No. Only completed origin responses in the relevant non-server-error class update the client-error signal. Gateway denials are separate outcomes.
10. **What if a request succeeds after its circuit has opened?** Its old generation ticket is ignored. Otherwise an unrelated old request could close a circuit whose probe is still failing.
11. **What if the half-open caller cancels?** Cancel releases the matching probe without blaming the origin. It can be probed again rather than remaining stuck.
12. **Why finalize around ReverseProxy's panic path?** A truncated body can abort after headers; normal return-path cleanup would be skipped. Capacity and probe ownership must still terminate.
13. **Can a recorded failure status always reach the client?** No. A body-stream abort may close a connection after an earlier status was sent. The HTTP contract distinguishes internal failure decisions from delivery guarantees.
14. **How does the load generator avoid counting partial success?** It reads the complete response body, checks errors and rejects invalid transport/body trials. Offered counts reconcile with actual receipts.
15. **What is actually deterministic in the campaign?** The offered schedule, cohort/path choices and seed. OS scheduling, response timing and final outcomes are measured and can vary.
16. **Why alternate fixed/adaptive order?** It reduces a consistent warm-host ordering bias, but does not replace a dedicated host, CPU affinity or independent evaluation.
17. **Does a better aggregate success rate establish fairness?** No. A particular client's arrival phase can still lose; inspect per-client receipts and controls. There is no fair queue.
18. **Why report admitted_pct in a microbenchmark?** Otherwise a tiny decision ns/op might be mistaken for full forwarded-request overhead. The saturated benchmark is mostly the denied path and omits network/feedback.
19. **What does restarting a replica do?** Local budgets, signals and counters reset. Several replicas multiply allowance; no shared/global quota is implemented.
20. **Why stop before integrating Pulse?** Its authentication, WebSocket lifetime and turn-level work boundary differ from this bounded HTTP proxy. Integration needs a small explicit adapter, regression measurements and separate approval.
