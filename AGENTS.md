# AGENTS.md

This is a Go CLI for planning, executing, and resuming an Apple Silicon macOS laptop setup run. `bootstrap.sh` downloads the latest `laptop-setup-darwin-arm64` release binary; the Go binary embeds files from `templates/`.

## Rules

- Keep `README.md` consumer-facing; put maintainer/developer guidance here.
- Prefer small, tested changes that follow existing package boundaries.
- Do not call `os/exec` directly from stages; use `runner.CommandRunner`.
- Put filesystem, template, Homebrew, state, TTY, UI, and execution seams behind the existing ports.

## Map

- `cmd/laptop-setup`: CLI entrypoint to `internal/app.Run`.
- `internal/app`: flag parsing, TTY requirement, dependency wiring, resume validation.
- `internal/ui`: Bubble Tea decisions and execution progress.
- `internal/execution`: plan execution, state transitions, retry/skip/abort behavior.
- `internal/stages`: stage catalog, decisions, prechecks, run/simulate functions, stage ports.
- `internal/state`: `~/.laptop-setup/state.json` schema and validation.
- `internal/runner`: command execution plus `run.log` and `events.jsonl`.
- `assets.go` and `templates/`: embedded release-time setup assets.

## Stages

When adding or changing a stage, update `stages.DefaultCatalog`, implement precheck before run behavior, keep dry-run non-mutating, and add focused tests for plan effects, precheck, run, simulation, missing prerequisites, and resume/idempotency where relevant. If decisions change, update `DecisionSet`, TUI handling, and resume validation tests.

## Verify

```shell
go test ./...
go vet ./...
GOOS=darwin GOARCH=arm64 go build -o laptop-setup-darwin-arm64 ./cmd/laptop-setup
```
