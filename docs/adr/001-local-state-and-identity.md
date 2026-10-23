# Local admission and configured client identities

Accepted. Build standard-library Go limiters with one bounded engine registry protected by a mutex. Keys are configured out of band and mapped to validated synthetic labels; do not trust a public client-ID header. This keeps concurrency arguments inspectable and avoids unbounded client cardinality. A mutex can contend at scale; the microbenchmark measures this path. Redis/distributed state is optional and deferred. Multiple instances or restarts multiply/reset local budgets; no global limit is claimed.
