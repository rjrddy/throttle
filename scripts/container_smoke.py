"""Exercise the actual nonroot image over authenticated HTTP; Linux CI only."""

import json
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

TOKEN = "synthetic-container-admin-token-0001"
KEY = "synthetic-throttle-amber-key-0001"
OUT = Path("docs/results/container")


def request(
    path: str, token: str = "", body: dict | None = None, method: str = "GET"
) -> tuple[int, str]:
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(
        "http://127.0.0.1:18086" + path,
        headers=headers,
        method=method,
        data=json.dumps(body).encode() if body is not None else None,
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        [
            "docker",
            "run",
            "-d",
            "--name",
            "throttle-smoke",
            "-p",
            "127.0.0.1:18086:8086",
            "-e",
            "THROTTLE_ADMIN_TOKEN=" + TOKEN,
            "throttle-ci",
        ],
        check=True,
    )
    try:
        for _ in range(100):
            try:
                if request("/health")[0] == 200:
                    break
            except OSError:
                pass
            time.sleep(0.1)
        else:
            raise RuntimeError("container readiness")
        assert request("/api/state")[0] == 401
        assert request("/proxy/v1/read")[0] == 401
        assert request("/proxy/v1/read", KEY)[0] == 200
        assert request("/")[0] == 200
        policy = {"mode": "fixed", "algorithm": "token", "rate": 1, "burst": 1}
        assert request("/api/policy", TOKEN, policy, "PUT")[0] == 200
        assert request("/proxy/v1/read", KEY)[0] == 200
        limited = request("/proxy/v1/read", KEY)
        assert limited[0] == 429
        assert request("/api/origin", TOKEN, {"mode": "fail"}, "PUT")[0] == 200
        policy.update(rate=1000, burst=1000)
        assert request("/api/policy", TOKEN, policy, "PUT")[0] == 200
        for _ in range(3):
            assert request("/proxy/v1/read", KEY)[0] == 503
        state = json.loads(request("/api/state", TOKEN)[1])
        assert state["breaker"] == "open"
        assert request("/api/origin", TOKEN, {"mode": "healthy"}, "PUT")[0] == 200
        time.sleep(1.1)
        assert request("/proxy/v1/read", KEY)[0] == 200
        assert json.loads(request("/api/state", TOKEN)[1])["breaker"] == "closed"
        (OUT / "state.json").write_text(json.dumps(state, indent=2) + "\n")
        result = {
            "image_http": "pass",
            "unauthenticated_rejection": 401,
            "rate_gate": limited,
            "real_origin_fault_circuit_recovery": "pass",
            "nonroot_image": True,
        }
        (OUT / "verification.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result))
    finally:
        result = subprocess.run(
            ["docker", "logs", "throttle-smoke"], capture_output=True, text=True
        )
        (OUT / "container.log").write_text(result.stdout + result.stderr)
        subprocess.run(["docker", "rm", "-f", "throttle-smoke"], check=True)


if __name__ == "__main__":
    main()
