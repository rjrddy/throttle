"""Read-only Go formatting gate using the selected toolchain's gofmt."""

import subprocess
from pathlib import Path

gorooot = subprocess.check_output(["go", "env", "GOROOT"], text=True).strip()
files = sorted(str(p) for folder in ["cmd", "internal"] for p in Path(folder).rglob("*.go"))
result = subprocess.check_output([str(Path(gorooot) / "bin/gofmt"), "-l", *files], text=True)
if result.strip():
    raise SystemExit("Unformatted Go source:\n" + result)
