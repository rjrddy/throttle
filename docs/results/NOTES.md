# Evidence notes

Final campaign/ contains one raw response-receipt file per scenario/algorithm/mode/trial and a compact summary indexing them. Each file records the seeded offered schedule, actual start/latency/status/outcome, reconciled classes and observed origin peak. report.json and the README/full comparison are generated from these files plus actual Go microbenchmark/test output. Summary receipt/history arrays are omitted to avoid duplicating raw files; their raw_file index names the complete evidence. No successful responses or throughput values are synthesized.

pilot/ is a short preliminary run and is excluded from the final report. The final comparison runs after local browser, tests, scans and builds, with no other owned test/load/browser workload active. It remains a short shared-laptop experiment without CPU affinity. Go test clocks/seeds are deterministic; real HTTP outcomes are not.

The admission microbenchmark prewarms signals, supplies a fixed virtual offered cadence and reports actual allowed_pct. It includes the admission lock/features/decision and excludes transport, authentication and response-event bookkeeping. Do not present its ns/op as total HTTP gateway overhead.

The browser's first run stopped because its exact Algorithm-label selector could not match the wrapping select text. initial-selector-failure.log is retained; explicit labels/IDs fixed the control association, and the full rerun passed. A failed initial recording, if present, is clearly separate from demo.webm. The final video contains actual repeated traffic/policy/fault work; its ffprobe metadata establishes duration. There are no padded frames or fake telemetry.

Caught system failures are negative tests, not erased runs: raw Go test output logs actual rate denial, origin-capacity rejection, body bound, real response-header timeout and truncated body handling. The envelope checker also rejects an intentionally invalid immediate batch. The healthy-unclassified case is a deliberately retained policy weakness, not a transport failure to remove from the report.

The measured binary may report a prior code commit with vcs.modified=true because result/report files were added afterward. platform.json records a SHA256 map of the measured Go source tree; it matches the committed core. The independent fresh clone receipt names its exact source commit and passed real seeded startup after build/lint/race checks.

Linux CI passed build/lint/race tests, then the old pinned govulncheck analyzer panicked in its SSA builder before completing its scan. initial-linux-scanner-failure.log is preserved. The scanner was updated to the verified current x/vuln v1.9.0 release; the source-reachability/advisory gate remains mandatory, with no exclusions or suppression. Local final scan and subsequent Linux CI establish the result. The gateway toolchain/core and measured source hashes were not changed by this tooling fix.
