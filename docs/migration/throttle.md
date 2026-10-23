# Throttle migration

Inspected the old Multi-provider LLM router (`projects/m2-multi-provider-router`) during the approved audit. Reused its testing ideas: bounded response timeout, repeated-failure opening, recovery after a cooldown and all-unavailable handling. The new Go HTTP tests use actual TCP origins/proxies rather than the old in-process httpx mock transport. The breaker adds fenced generations, one half-open probe and aborted-response/cancel cleanup. These are independent implementations, not copied employer interfaces or code.

Rewritten from scratch: API-key identity registry, all limiter algorithms, adaptive score and training boundaries, capacity semaphore, reverse proxy/header handling, Go race/property tests, seeded load generator, React console and benchmark/report tooling. Provider-specific prompt/completion interfaces and fallback logic are not carried forward. Throttle forwards to one explicitly configured trusted origin and does not replay application requests.

The old repository remains untouched and no runtime/build dependency points into projects/. No deletion is authorized by this document. Pulse remains untouched. A Throttle → Pulse integration may be proposed only after Throttle passes its own gates and needs a separate user approval.
