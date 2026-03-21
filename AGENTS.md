# Repository Guidelines

## Project Structure & Module Organization
`main.go` starts the server and boots sessions from YAML config. Backend code lives under `internal/`: `config/` for YAML loading and validation, `api/` for REST handlers, `ws/` for WebSocket terminal bridging, `server/` for router/static serving, and `session/` for local, SSH, and tmux session implementations. The React frontend lives in `frontend/src/`, with `components/`, `hooks/`, `schemas/`, `types/`, and `utils/`. Keep sample configuration in `config.example.yaml`; built artifacts go to `bin/` and `frontend/dist/`.

## Build, Test, and Development Commands
Use `make install-deps` on a fresh clone to install frontend packages and Go modules, or `make install-deps-ci` in CI. Run `make check` for the full quality gate: lint, tests, and coverage. Use `make build` to produce `bin/panemux`; it depends on a successful `make check`. For local development, run `make dev-backend` and `make dev-frontend` in separate terminals. Use `make release-snapshot` to produce local release archives through GoReleaser.

## Coding Style & Naming Conventions
Follow idiomatic Go and keep files `gofmt`-formatted; exported identifiers use `CamelCase`, unexported helpers use `camelCase`. In the frontend, match the existing TypeScript style: 2-space indentation, single quotes, and no semicolons. React components use `PascalCase` filenames like `TerminalPane.tsx`; hooks use `useX.ts`; tests sit beside the code as `*.test.ts` or `*_test.go`. Update schemas before derived types: change `frontend/src/schemas/index.ts` instead of editing generated TypeScript types manually.

## Testing Guidelines
This repo follows TDD: write or update tests first, then implement. Go tests use `testing` with `testify`; frontend tests use Vitest and Testing Library. Maintain at least 80% coverage for the guarded packages and hooks/schema targets enforced by `make coverage-go` and `cd frontend && npm run coverage`. When changing config or payload shapes, update validation tests in `internal/config/validate.go` and schema tests in `frontend/src/schemas/index.test.ts`.

## Commit & Pull Request Guidelines
Recent history uses short imperative commit subjects such as `fix window resize bug` and `Add dynamic pane management and pane header/status bar`. Keep commits focused and descriptive. Pull requests should explain the user-visible change, note any config or protocol updates, link related issues, and include screenshots or terminal captures for frontend/layout changes. Confirm `make check` passes before opening the PR.

## Documentation Rule
When behavior, operational assumptions, browser requirements, rendering constraints, or other user-visible rules become confirmed, update the relevant files under `docs/` in the same change. Do not defer settled documentation updates to a later task.

## CI Rule
Pin GitHub Actions workflows to full commit SHAs, not floating major tags such as `@v4` or `@v5`. When adding or updating workflow actions, resolve the tag to its current commit SHA and record that exact SHA in the workflow file.

## Ignore Rule
When introducing new generated artifacts, caches, release outputs, or other non-source resources, update `.gitignore` in the same change. Build and release byproducts such as `dist/` must not be left as untracked clutter.
