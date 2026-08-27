.PHONY: all build build-frontend build-backend dev clean run install-deps install-deps-ci install-hooks \
        test test-go test-frontend test-e2e test-agmsg-contract test-efficacy efficacy \
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

test: test-go test-frontend test-efficacy

test-go:
	go test ./... -v -race

test-frontend:
	cd frontend && npm test

test-e2e:
	cd frontend && npm run test:e2e

# ── Efficacy: red-check (gate G4(b)) ──────────────────────────────────────────
#
# Asserts decision D4: a test this branch changed must FAIL when this branch's
# implementation diff is reverted. That is the strongest possible mutation —
# remove the implementation entirely — and it catches the two shapes the other
# gates are blind to: a tautological test, and a test written after the fact.
#
# Every changed test is judged on its own, in two phases: pass at HEAD, then
# fail against the revert. One invocation over the whole set would be a weaker
# rule, since `go test` goes red if any single member does.
#
# Deliberately OUTSIDE `make check`. It needs the base branch, a scratch
# worktree and a second test run, which is a pull-request-shaped cost rather
# than an every-turn one — the same reasoning that keeps mutation testing (D2)
# and the agmsg contract out of the local gate. It runs as its own PR CI job.
#
#   make efficacy                          # against origin/main
#   EFFICACY_BASE=origin/develop make efficacy
#   EFFICACY_EXEMPT=1 make efficacy        # documented escape hatch; say why in the PR
efficacy:
	sh scripts/efficacy.sh

# The red-check's own tests. Unlike `make efficacy` these are hermetic — they
# drive the script against throwaway git repositories — so they belong in
# `make test` alongside everything else.
test-efficacy:
	sh scripts/efficacy_test.sh

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
# The threshold is deliberately NOT raised above 80 %: see decision D1 in
# docs/quality-gateway.md. Coverage is only meaningful as a lower bound, and
# the cheapest way to satisfy a higher one is to generate tautological tests,
# which lowers protection against regressions and resistance to refactoring at
# the same time. What gets strengthened is the SCOPE below, never the number.
#
# Go: measures every package that holds a decision — config, api, ws, server,
#     board, portforward, commandcenter, boardmcp, and the root package
#     (main.go's startup path plus board.go / bootstrap.go / command_center.go
#     / board_mcp_server.go).
#
#     Deliberately excluded, with reasons rather than by omission:
#
#       internal/session   its lifecycle methods drive a real PTY (local,
#                          tmux), a live SSH connection (ssh, ssh_tmux) and a
#                          real tmux server. `make check` is hermetic —
#                          principle 5 in docs/quality-gateway.md — so these
#                          are exercised by the package's own tests and by
#                          E2E, not gated here. The pure decisions extracted
#                          out of them (validateShell, validRemotePath,
#                          classifySSHWaitError …) are unit-tested in place.
#       internal/sshconfig  a parser over the user's own ~/.ssh/config; it has
#                          its own tests and no gate-worthy branching that the
#                          packages above do not already drive.
#
#     Within the measured set, main()/runServer()/bootstrapWatcher.Run() stay
#     uncovered for the same reason: they install signal handlers, start the
#     listener and run poll loops for the life of the process. Every decision
#     they used to embed has been extracted into the injectable units around
#     them (parseOptions, loadConfig, startSessionsFromConfig,
#     browserOpenArgv), which is where the gate applies.
#
# Frontend: measured over src/hooks/, src/schemas/ and src/utils/.
#           UI components (App, SplitContainer, TerminalPane …) require a real
#           browser renderer and are covered by integration / E2E tests.

COVERAGE_PKGS := ./internal/config/...,./internal/api/...,./internal/ws/...,./internal/server/...,./internal/board/...,./internal/portforward/...,./internal/commandcenter/...,./internal/boardmcp/...,.

coverage: coverage-go coverage-frontend

# build-frontend is a real prerequisite, not tidiness: the root package joined
# the gate above, and main.go carries `//go:embed frontend/dist`, so compiling
# it needs that directory to exist. Without this, `make coverage-go` on a clean
# tree fails with "pattern frontend/dist: no matching files found" — an error
# that gives no hint the fix is to build the frontend first. `make check` and
# CI already build it, so this costs them nothing and only makes the
# standalone command DEVELOPMENT.md documents work on its own.
coverage-go: build-frontend
	go test \
	  ./internal/config/... \
	  ./internal/api/... \
	  ./internal/ws/... \
	  ./internal/server/... \
	  ./internal/board/... \
	  ./internal/portforward/... \
	  ./internal/commandcenter/... \
	  ./internal/boardmcp/... \
	  . \
	  -coverprofile=coverage.out \
	  -coverpkg=$(COVERAGE_PKGS) \
	  -count=1 -timeout 60s
	@pct=$$(go tool cover -func=coverage.out | grep "^total:" | awk '{gsub(/%/,""); print $$3}'); \
	  printf "Go coverage (gated packages): %s%%\n" "$$pct"; \
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
