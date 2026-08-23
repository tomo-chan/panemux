.PHONY: all build build-frontend build-backend dev clean run install-deps install-deps-ci install-hooks \
        test test-go test-frontend test-e2e test-agmsg-contract test-hooks \
        fmt fmt-go fmt-check-go \
        lint lint-go lint-go-deps lint-frontend \
        coverage coverage-go coverage-frontend \
        check release-snapshot package

GOLANGCI_LINT_VERSION := v2.12.2

# Resolve the exact path `go install` places golangci-lint at (GOBIN if set,
# otherwise $GOPATH/bin), and always invoke that path explicitly rather than
# a bare `golangci-lint` from $PATH. A bare invocation can silently resolve
# to an unrelated, unpinned golangci-lint earlier on $PATH (e.g. one
# preinstalled system-wide) even after this file's own install step just
# put the correct pinned version at $GOBIN/$GOPATH/bin — the install would
# succeed and the very next lint run would still lint with the wrong
# binary. Pinning the invocation path removes that ambiguity entirely.
GOLANGCI_LINT_BIN := $(shell bin="$$(go env GOBIN)"; if [ -z "$$bin" ]; then bin="$$(go env GOPATH)/bin"; fi; echo "$$bin")/golangci-lint

# ── Dependencies ──────────────────────────────────────────────────────────────

install-deps: lint-go-deps install-hooks
	cd frontend && npm install
	go mod download

install-deps-ci: lint-go-deps
	cd frontend && npm ci
	go mod download

install-hooks:
	chmod +x .githooks/pre-push
	git config core.hooksPath .githooks

# ── Tests ─────────────────────────────────────────────────────────────────────

test: test-go test-frontend test-hooks

test-go:
	go test ./... -v -race

test-frontend:
	cd frontend && npm test

test-e2e:
	cd frontend && npm run test:e2e

# The agent-side gates themselves (docs/quality-gateway.md's G1 and G2, run as
# Claude Code hooks from .claude/). Included in `make test` because a hook that
# silently stops working reports the discipline as enforced while enforcing
# nothing — and because a hook that blocks a healthy change is worse still, so
# both directions are asserted. Hermetic: it drives the scripts against temp
# files and throwaway git repositories, never this checkout.
test-hooks:
	sh .claude/hooks/hooks_test.sh

# Tier 2 of docs/agent-board.md's agmsg compatibility contract: asserts the
# agmsg script behaviors panemux depends on against a REAL agmsg install.
# Deliberately outside `make check`, which must stay hermetic — the tests
# skip themselves when AGMSG_PATH is unset.
#
#   make test-agmsg-contract AGMSG_PATH=~/.agents/skills/agmsg
AGMSG_PATH ?=
test-agmsg-contract:
	PANEMUX_AGMSG_PATH="$(AGMSG_PATH)" go test ./internal/board/ -run 'TestAgmsgContract' -v -count=1

# ── Coverage (≥ 80 %) ─────────────────────────────────────────────────────────
#
# Go: measures config / api / ws / server / board (business-logic packages).
#     session/local uses a real PTY and is covered separately.
#     session/ssh and session/tmux* require live SSH / tmux and are
#     integration-tested outside the unit-test suite.
#
# Frontend: measured over src/hooks/ and src/schemas/ only.
#           UI components (App, SplitContainer, TerminalPane …) require a real
#           browser renderer and are covered by integration / E2E tests.

COVERAGE_PKGS := ./internal/config/...,./internal/api/...,./internal/ws/...,./internal/server/...,./internal/board/...

coverage: coverage-go coverage-frontend

coverage-go:
	go test \
	  ./internal/config/... \
	  ./internal/api/... \
	  ./internal/ws/... \
	  ./internal/server/... \
	  ./internal/board/... \
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

lint-go-deps:
	@expected_version="$$(printf '%s' '$(GOLANGCI_LINT_VERSION)' | sed 's/^v//')"; \
	current_version="$$(test -x '$(GOLANGCI_LINT_BIN)' && '$(GOLANGCI_LINT_BIN)' --version 2>/dev/null | awk 'NR==1 {print $$4; exit}' || true)"; \
	if [ "$$current_version" = "$$expected_version" ]; then \
	  :; \
	else \
	  echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) to $(GOLANGCI_LINT_BIN)"; \
	  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi

lint-go: fmt-check-go lint-go-deps
	go vet ./...
	'$(GOLANGCI_LINT_BIN)' run ./...

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
