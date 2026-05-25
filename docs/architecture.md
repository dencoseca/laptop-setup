# Architecture

This project is a Go command for planning, executing, and resuming a macOS laptop setup run. The command keeps CLI behavior small, persists enough state to resume safely, and records both human-readable and structured run logs.

## Package Boundaries

- `cmd/laptop-setup`: CLI entrypoint. It delegates to `internal/app.Run`.
- repository root package: embeds release-time assets such as setup templates for standalone binaries.
- `internal/app`: application orchestration. It parses flags, opens state, validates resume requests, requires an interactive TTY, and wires production dependencies through explicit ports.
- `internal/domain/setup`: typed domain values shared across packages, including run IDs, modes, stage IDs, and stage status values.
- `internal/state`: JSON state persistence. It owns the on-disk state schema, run ID validation, state validation, and filesystem state store.
- `internal/execution`: execution engine. It walks the resolved plan, calls stage prechecks and runners, persists status transitions, emits lifecycle events, and applies retry, skip, or abort failure actions.
- `internal/stages`: stage catalog and stage-level ports. It defines `Stage`, `ExecutionContext`, `FileSystem`, `TemplateStore`, and `PackageManager`, plus the current macOS/Homebrew/shell/git stage implementations.
- `internal/runner`: command execution and run logging infrastructure. It defines the command runner port, the OS runner, typed command errors, and JSONL event logging.
- `internal/ui`: Bubble Tea TUI. It collects user decisions, renders execution progress, and calls app/execution services through injected options.

The dependency direction should stay mostly one-way: `app` coordinates packages, `execution` consumes stage definitions and state, `stages` consumes runner/domain types, and `ui` should not own persistence or shell behavior.

## Ports

The main test seams are:

- `app.StateRepository` and `state.Store` for state load/save.
- `runner.CommandRunner` for command execution and executable lookup.
- `stages.TemplateStore` for embedded or repository templates and generated run files.
- `stages.PackageManager` for Homebrew availability and bundle execution.
- `app.PathResolver`, `app.RunLogFactory`, `app.Executor`, `app.UIRunner`, and `app.TTYDetector` for application-level orchestration tests.

New infrastructure behavior should be added behind one of these ports, or behind a similarly small interface when the behavior crosses an OS, filesystem, network, or package-manager boundary.

## State File Schema

The default state file is:

```text
~/.laptop-setup/state.json
```

Current JSON fields:

```json
{
  "run_id": "20260525T120000000000000Z-abc123...",
  "start_at": "2026-05-25T12:00:00Z",
  "end_at": "2026-05-25T12:03:00Z",
  "mode": "normal",
  "decisions": {
    "selected_stage_ids": ["xcode_clt", "brew_bundle"],
    "dev.node_toolchain": "vite_plus",
    "dev.docker_runtime": "colima",
    "shell.install_oh_my_zsh": true,
    "shell.apply_zshrc_template": true,
    "shell.apply_starship_template": true,
    "git.config_mode": "template",
    "git.user_name": "",
    "git.user_email": ""
  },
  "selected_ids": ["go", "jq"],
  "resolved_plan": ["xcode_clt", "brew_bundle"],
  "stages": {
    "brew_bundle": {
      "status": "success",
      "attempts": 1
    }
  },
  "last_failure": "",
  "generated_file": "/Users/me/.laptop-setup/runs/<run-id>/Brewfile.generated"
}
```

Validation rules live in `internal/state.ValidateRunState` and `internal/execution.ValidateRunStateForCatalog`. Persisted state must have a valid run ID, mode, non-empty resolved plan, known status values, valid decisions, and no unknown stage IDs on resume. `generated_file`, when present, must be an absolute clean path inside the run directory.

Stage statuses are:

- `pending`
- `running`
- `success`
- `skipped`
- `failed`
- `already_done`
- `simulated_success`

Terminal statuses are success, skipped, already done, and simulated success. Resume skips terminal stages and retries failed or running stages according to the saved plan.

## Run Directory

Each run has a directory under:

```text
~/.laptop-setup/runs/<run-id>/
```

Expected contents:

- `run.log`: human-readable lifecycle, command, and error log.
- `events.jsonl`: structured event stream. Each line is one `runner.Event` JSON object.
- `Brewfile.generated`: run-scoped Brewfile created for `brew_bundle` when applicable.

Use `run_id` from `state.json` to connect persisted state to its run directory. Event type string values are centralized in `internal/runner/logger.go` and should remain JSON-compatible.

## Adding A Stage

1. Add a `Stage` entry to `stages.DefaultCatalog` with a stable `ID`, title, description, dependency decision keys, skip policy, criticality, log tag, precheck, run, and simulate functions.
2. Implement the precheck first. It should return satisfied when the target machine state already matches the intended result.
3. Put shell commands through `runner.CommandRunner` via `runCommand`; do not call `os/exec` directly from a stage.
4. Put filesystem work through `stages.FileSystem` and template reads through `stages.TemplateStore`.
5. Put Homebrew behavior through `stages.PackageManager`.
6. Keep dry-run behavior non-mutating in the stage `Simulate` function.
7. Add or update plan tests if the new stage affects ordering, `--from`, `--only`, or `--skip`.
8. Add stage tests for precheck satisfied, run behavior, dry-run behavior, missing prerequisites, and idempotent re-runs when practical.
9. If the stage adds user-facing decisions, update `DecisionSet` validation, TUI option handling, resume validation tests, and this document if the state schema changes.

## Maintainer Checklist

Before a release:

```shell
go test ./...
go vet ./...
GOOS=darwin GOARCH=arm64 go build -o laptop-setup-darwin-arm64 ./cmd/laptop-setup
```

Attach `laptop-setup-darwin-arm64` to the GitHub release. `bootstrap.sh`
downloads that asset directly; it does not install Go, clone the repository, or
build on the target machine.

Also run targeted tests for changed areas, for example:

```shell
go test ./internal/app ./internal/execution ./internal/stages ./internal/state ./internal/runner
go test ./internal/ui
```

Manual checks:

- Run `laptop-setup --dry-run` from a local build.
- Run a one-stage interactive path with `--only <stage-id>` for changed stages.
- Validate `state.json`, `run.log`, and `events.jsonl` are created and readable.
- Validate `--resume` after a forced failure.
- Use [VM Smoke Test Checklist](vm-smoke-test-checklist.md) for full clean-machine validation.
- Use [Operations Troubleshooting](operations-troubleshooting.md) for failure triage expectations.
