.PHONY: all build build-frontend build-backend dev clean run install-deps install-deps-ci \
        test test-go test-frontend \
        fmt fmt-go fmt-check-go \
        lint lint-go lint-frontend \
        coverage coverage-go coverage-frontend \
        check release-snapshot package

# ── Dependencies ──────────────────────────────────────────────────────────────

install-deps:
	cd frontend && npm install
	go mod download

install-deps-ci:
	cd frontend && npm ci
	go mod download

# ── Tests ─────────────────────────────────────────────────────────────────────

test: test-go test-frontend

test-go:
	go test ./... -v -race

test-frontend:
	cd frontend && npm test

# ── Coverage (≥ 80 %) ─────────────────────────────────────────────────────────
#
# Go: measures config / api / ws / server (business-logic packages).
#     session/local uses a real PTY and is covered separately.
#     session/ssh and session/tmux* require live SSH / tmux and are
#     integration-tested outside the unit-test suite.
#
# Frontend: measured over src/hooks/ and src/schemas/ only.
#           UI components (App, SplitContainer, TerminalPane …) require a real
#           browser renderer and are covered by integration / E2E tests.

COVERAGE_PKGS := ./internal/config/...,./internal/api/...,./internal/ws/...,./internal/server/...

coverage: coverage-go coverage-frontend

coverage-go:
	go test \
	  ./internal/config/... \
	  ./internal/api/... \
	  ./internal/ws/... \
	  ./internal/server/... \
	  -coverprofile=coverage.out \
	  -coverpkg=$(COVERAGE_PKGS) \
	  -count=1 -timeout 30s
	@pct=$$(go tool cover -func=coverage.out | grep "^total:" | awk '{gsub(/%/,""); print $$3}'); \
	  printf "Go coverage (business-logic packages): %s%%\n" "$$pct"; \
	  awk -v p="$$pct" 'BEGIN { if (p+0 < 80) { print "FAIL: coverage "p"% is below 80%"; exit 1 } }'

coverage-frontend:
	cd frontend && npm run coverage

# ── Format ────────────────────────────────────────────────────────────────────

fmt: fmt-go

fmt-go:
	gofmt -s -w .

fmt-check-go:
	@files=$$(gofmt -s -l .); \
	if [ -n "$$files" ]; then \
	  echo "Unformatted Go files (run 'make fmt'):"; \
	  echo "$$files"; \
	  exit 1; \
	fi

# ── Lint ──────────────────────────────────────────────────────────────────────

lint: lint-go lint-frontend

lint-go: fmt-check-go
	go vet ./...

lint-frontend:
	cd frontend && npx tsc --noEmit

# ── Quality gate (lint + test + coverage) ─────────────────────────────────────

check: build-frontend lint test coverage

# ── Build ─────────────────────────────────────────────────────────────────────

# build requires check (build-frontend + lint + test + coverage) to pass first.
build: check build-backend

build-frontend:
	cd frontend && npm run build

LDFLAGS := -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build-backend:
	go build -ldflags "$(LDFLAGS)" -o bin/panemux .

# ── Release packaging ─────────────────────────────────────────────────────────

release-snapshot:
	GITHUB_REPOSITORY_OWNER=$${GITHUB_REPOSITORY_OWNER:-local} \
	GITHUB_REPOSITORY_NAME=$${GITHUB_REPOSITORY_NAME:-panemux} \
	goreleaser release --snapshot --clean

release-check:
	GITHUB_REPOSITORY_OWNER=$${GITHUB_REPOSITORY_OWNER:-local} \
	GITHUB_REPOSITORY_NAME=$${GITHUB_REPOSITORY_NAME:-panemux} \
	goreleaser check

package: release-snapshot

# ── Dev ───────────────────────────────────────────────────────────────────────

dev-backend:
	go run . --port 8080

dev-frontend:
	cd frontend && npm run dev

# ── Run ───────────────────────────────────────────────────────────────────────

run: build
	./bin/panemux

run-config: build
	./bin/panemux --config config.example.yaml --open

# ── Clean ─────────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/ frontend/dist/ coverage.out
