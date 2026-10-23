# Capacity leases, fenced recovery and no automatic application retry

Accepted. Bound origin concurrency without a queue, and fence half-open circuit probes with an epoch. A stale successful completion cannot recover an open circuit. Always release a capacity lease and a failed/aborted stream's probe. Do not add automatic application retries or fallback for arbitrary forwarded requests because a POST may already have taken effect. A future failover feature needs idempotency/replay semantics and independent fault tests.
