# A ninety-second real Throttle demo

Run make demo, then open http://127.0.0.1:8086. The console is React served by the real Go gateway; all counts, scores and receipts come from that process. No account, public deployment or separate Redis is needed.

Suggested recording timeline (instructions, not performance claims):

- 0–15 seconds: show the registered clients, origin-concurrency cap and empty actual timeline. Explain that identity comes from configured keys, not X-Client-ID.
- 15–30 seconds: choose fixed/token bucket, Apply policy, then Run synthetic traffic. Inspect actual legitimate successes, capacity rejections and rate denials.
- 30–45 seconds: choose adaptive/token bucket, apply and rerun. Select an abuse client to inspect pressure/error/mix and budget; select a legitimate client to show bursts alone retained its base budget. Lifetime counters span both runs; the campaign result is scoped to that run.
- 45–60 seconds: select rolling window or GCRA and run again. Explain the different rolling-window burst semantics instead of treating them as identical.
- 60–75 seconds: inject fail into the synthetic origin and send requests using a configured fixture key. Show real failure receipts, circuit opening and recovery after healthy/probe. Do not send synthetic load to an external service.
- 75–90 seconds: show the saved benchmark comparison and healthy-unclassified case where the heuristic hurts; then the race/property gates and actual fault receipts.

A CLI request from a second terminal: curl -H 'Authorization: Bearer synthetic-throttle-amber-key-0001' http://127.0.0.1:8086/proxy/v1/read. This is a public synthetic fixture, not a secret.

Optional automated recorder: install requirements-browser.txt into the project virtualenv, run .venv/bin/python -m playwright install chromium ffmpeg, then .venv/bin/python scripts/review.py --record. It starts/stops its own temporary server, executes real fixed/adaptive/algorithm campaigns and fault/recovery, and records repeated actual work for a genuine long demo. PLAYWRIGHT_BROWSERS_PATH may point to an existing compatible cache. Raw video and browser receipts go under docs/results/browser. Use ffprobe to verify the encoded duration; never pad short recordings or replace counters with fabricated animation.
