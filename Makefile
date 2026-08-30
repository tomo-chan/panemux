.PHONY: all build build-frontend build-backend dev clean run install-deps install-deps-ci install-hooks \
        test test-go test-frontend test-e2e test-agmsg-contract test-hooks test-efficacy efficacy \
        test-scenarios-check check-scenarios coverage-blocks test-coverage-blocks \
        mutation test-mutation bench \
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

test: test-go test-frontend test-hooks test-efficacy test-scenarios-check test-coverage-blocks \
      test-mutation

test-go:
	go test ./... -v -race

test-frontend:
	cd frontend && npm test

test-e2e:
	cd frontend && npm run test:e2e

# ── Performance observation (not a gate) ──────────────────────────────────────
#
# Performance efficiency is one of the two ISO 25010 characteristics
# docs/quality-gateway.md records as completely unprotected. These benchmarks
# are the first measurement of it: terminal output throughput and replay-buffer
# cost (internal/session), and the relay's continuous polling cost
# (internal/board).
#
# Deliberately NOT in `make check` and deliberately asserting nothing. Roadmap
# item 7 of issue #180 is explicit that this stage is measure-only — a
# threshold guessed at now would be a number nobody trusts, and an untrusted
# gate is worse than none. Freeze one once a few runs' worth of data exists.
#
# Use -count for anything you intend to read: on a shared container the publish
# rows move by up to 2.9x between runs of the same binary, so a single run says
# almost nothing. The whole suite is ~35s at the default benchtime and ~3min at
# -count 5.
#
#   make bench
#   make bench BENCH_ARGS='-count 5'                 # medians and a spread
#   make bench BENCH_ARGS='-benchtime 3s -count 5'   # slower, steadier
BENCH_ARGS ?=
bench:
	go test ./internal/session/ ./internal/board/ -run '^$$' -bench . -benchmem $(BENCH_ARGS)

# ── Scenario ledger (gates G0 / G5) ───────────────────────────────────────────
#
# Resolves every path and Go test name that an `auto` row in docs/scenarios.md
# claims, and fails when one does not exist. The ledger's own rule already says
# a silently absent row is not a legitimate answer; this closes the failure that
# rule never anticipated — a row naming a test that has since been renamed,
# moved or deleted. Such a row reads as coverage and is worth nothing.
#
# Hermetic and fast, so it sits inside `make check`.
check-scenarios:
	sh scripts/scenarios_check.sh

# The checker's own tests. Its value rests on its false-positive rate — it
# reads prose, which is full of things shaped like paths that are not — so both
# directions are asserted.
test-scenarios-check:
	sh scripts/scenarios_check_test.sh

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

# ── Per-block coverage (gate G4(d), issue #164) ───────────────────────────────
#
# The statement percentage below cannot see a whole `if err != nil { ... }`
# body that no test ever enters: the happy path around it carries the function
# past 80% on its own. Issue #164 found 28 such branches by hand. This reads
# the same coverage.out and reports every block whose execution count, SUMMED
# across the profile's duplicate entries for it, is zero.
#
# Two shapes, and the difference matters:
#
#   make coverage-blocks                                  # report, exits 0
#   COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks # gate, exits 1 on a finding
#
# The gate speaks only about blocks covering a line the branch changed. The
# repository has ~275 unexecuted blocks today, so a gate over all of them would
# start red — and docs/quality-gateway.md principle 4 is that a gate which
# starts red gets routed around, taking the working gates with it. Diff-scoped,
# it starts green and stays cheap. Same reasoning as decision D2.
#
# Outside `make check` for the reason `make efficacy` is: it needs the base
# branch. It runs as its own pull-request CI job.
coverage-blocks: coverage-go
	sh scripts/coverage_blocks.sh

# The reporter's own tests, hermetic like the red-check's: fixture profiles and
# throwaway git repositories, no `go test` run needed.
test-coverage-blocks:
	sh scripts/coverage_blocks_test.sh

# ── Mutation: would the tests notice? (gate G4(c)) ────────────────────────────
#
# The last of the four G4 questions, and the one the other three cannot answer.
# (a) asks what percentage of statements ran, (d) asks whether a block on a
# changed line ran at all, (b) asks whether a CHANGED TEST fails without its
# implementation. This asks whether the tests would notice changed code
# behaving differently — and #180's measurement found 108 mutants in this
# repository that survive every test, all of them in code (d) reports as
# covered.
#
# A WARNING, not a gate: a survivor prints and exits 0. Stage 3 of item 6's
# four. 34% of the measured survivors are ones nobody should "fix" — buffer
# sizes and timeout constants whose killing test would be a tautology — so
# failing on them would make this wrong more often than right, which is how a
# gate loses the credibility the working ones depend on (principle 4). Making
# it fail is stage 4, and a deliberate separate change.
#
# Needs gremlins, which `make install-deps` does not install:
#   go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
#
# Outside `make check` for the reason `make efficacy` and `make coverage-blocks`
# are: it needs the base branch. It runs as its own pull-request CI job.
#
#   MUTATION_BASE=origin/main make mutation
mutation:
	sh scripts/mutation.sh

# The reporter's own tests. Hermetic, and deliberately so: they drive the
# script through `--report <file>` against fixture reports, so `make check`
# never needs gremlins installed.
test-mutation:
	sh scripts/mutation_test.sh

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
	@sh scripts/coverage_blocks.sh --summary

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

check: build-frontend lint test coverage check-scenarios

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
