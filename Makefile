GO ?= go
DEMO_ADDR ?= 127.0.0.1:8086
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/gobuild
export GOTOOLCHAIN := go1.27.3
.PHONY: setup build lint test bench bench-core demo scan container-test
setup:
	python3 -m venv .venv
	.venv/bin/python -m pip install -r requirements-dev.txt
	$(GO) mod download
	npm ci --prefix apps/web
build:
	$(GO) build -trimpath -o bin/throttle ./cmd/throttle
	npm run build --prefix apps/web
lint:
	python3 scripts/check_go_format.py
	$(GO) vet ./...
	.venv/bin/ruff check scripts
	.venv/bin/ruff format --check scripts
	npm run lint --prefix apps/web
	npm exec --prefix apps/web -- prettier --check apps/web/src
test:
	@mkdir -p docs/results
	$(GO) test -race -count=1 -json ./... > docs/results/tests.jsonl
	python3 scripts/summarize_tests.py
bench-core:
	$(GO) test ./internal/gateway -run '^$$' -bench BenchmarkAdmission -benchmem -benchtime=200ms -count=5 > docs/results/admission-bench.txt
bench: build bench-core
	./bin/throttle --bench --out docs/results/campaign > docs/results/campaign-run.log
	python3 scripts/render_readme.py
demo: setup build
	./bin/throttle --demo --addr "$(DEMO_ADDR)"
scan:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	npm audit --prefix apps/web --audit-level=high
	.venv/bin/python -m pip_audit -r requirements-dev.txt -r requirements-browser.txt --progress-spinner off
container-test:
	docker build -t throttle-ci .
	python3 scripts/container_smoke.py
