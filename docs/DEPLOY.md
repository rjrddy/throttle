# Hosting preparation — not deployed

Local one-command run: make demo. It builds the Go binary and React assets, starts the actual loopback gateway and its separate loopback synthetic origin, and prints the URL. Synthetic API keys are fixtures returned by DemoKeys in server.go. Ctrl-C shuts down the owned servers.

Docker: supply your own THROTTLE_ADMIN_TOKEN of at least 24 characters through the environment, then run docker compose up --build. Enter that token in the console. The image runs as a nonroot user; Docker/real circuit and admission smoke tests run in GitHub CI. Docker is unavailable on the local development Mac, so native tests are not substituted for container evidence.

render.yaml is a prepared, unactivated Free-plan Docker web-service Blueprint. The [provider's free-service documentation](https://render.com/docs/free) describes its current limits. After explicit owner approval, connect the repository as a Blueprint, review plan/region, deploy and obtain the generated admin token privately. No service/account has been created and no deployment was performed by this implementation.

The prepared image is a synthetic demonstration with its own local origin and public fixture client keys. It is not a reverse proxy for production traffic. Free-plan sleeping/replacement resets all local limiter/breaker/signal/counter state. Multiple instances have independent budgets and multiply the total allowance. Redis mode is optional and unimplemented. TLS termination belongs at the host; native listening is plaintext. Never infer hosted capacity, persistence, cross-instance fairness or service availability from the local benchmark.

For a real single-instance configuration, omit --demo, supply an explicit trusted --upstream HTTP origin, set THROTTLE_CLIENT_KEYS as client-label→secret JSON, and keep admin secrets in the environment. Review endpoint classification, key provisioning, upstream authorization and the harmful-unclassified workload before enabling adaptation. Arbitrary multi-tenant public traffic, fair queues and voice-stream accounting require additional work. Pulse integration is a separate proposal, not included.
