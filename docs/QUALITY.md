# Quality checkpoint

| Gate | State / evidence |
|---|---|
| Hard core written and explained | PASS: internal/gateway; docs/DESIGN.md; local-state/score/circuit ADRs |
| One-command fresh-clone run | Pending independent clone check |
| Live React behavior | PASS: docs/results/browser/verification.json; actual policy/load/fault/recovery, responsive layouts, keyboard client selection |
| Unit + integration + property/chaos | PASS: race-test JSON; limiter envelopes, concurrent hot identity, actual HTTP auth/overload/timeout/truncated-body cases |
| Benchmark numbers saved | Final isolated comparisons running; pilot retained separately |
| Demonstrated caught failure | Actual HTTP rate/capacity/body/timeout and truncated-response fault logs |
| ADRs | PASS: docs/adr/ |
| INTERVIEW.md and DRILL.md | PASS: code-grounded explanation and explicit unfavorable case |
| Dependency scan | PASS: Go source reachability scan plus npm advisory audit; final Python helper scan included |
| CI / Docker | Prepared; GitHub execution pending; Docker unavailable locally |
| No unmeasured claims | README generated from raw results; core overhead distinguished from HTTP latency and real-world abuse accuracy |

Redis/distributed mode is optional and not implemented. No deployment/account is created. No production fairness, cross-instance quota, identity federation, fair queue or long-lived voice-turn integration is claimed. Pulse remains unchanged and has a separate integration proposal requiring approval. Not deployed; the Render free-plan file is preparation only.
