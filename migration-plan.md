# Laptop Setup TUI Migration Plan

## Objective
Replace the current `setup.sh`-driven flow with a robust, resumable, interactive TUI application built in Go using Bubble Tea, while preserving the current setup behavior and granular execution stage boundaries.

## Why This Direction
- Fresh-machine friendly: ship a single statically linked binary.
- Minimal prerequisites: bootstrap can run with macOS built-ins (`curl`, `sh`, `chmod`).
- Better reliability: structured stages, state persistence, and explicit retry/skip flows.
- Better UX: clear progress, confirmation at each stage, and readable failure diagnostics.

## End-State Goals
1. `bootstrap.sh` downloads and runs the correct release binary for host architecture (`arm64`/`amd64`).
2. The binary provides an interactive TUI with phase-level decision prompts and per-stage live progress.
3. Non-interactive mode remains supported (`--yes`) for unattended execution.
4. Every stage is resumable and emits logs into a single per-run log stream (human + structured).
5. Existing templates remain source-of-truth inputs for generated config files.
6. User choices are collected up front, rendered as a concrete execution plan, then executed as granular stages.
7. Brew package/app installation is driven by a generated per-run Brewfile created from the user's confirmed selection.

## Messaging and Tone Policy
- Do not carry forward humorous runtime/status copy from the current shell flow (including `templates/loading-messages.txt`).
- Error and status messages in the migrated CLI/TUI must be concise, actionable, and professional (for example, avoid messages like `Congratulations, you managed to screw up copying and pasting a command!`).
- One intentional exception: keep the existing humorous CLI description text in `README.md` after migration:

```text
Provides the perfect environment to ignore time, hunger, friends and family,
most of life's responsibilities, stop blinking entirely, and eventually be able
to afford a second home, somewhere in the mediterranean, where you can talk
to your neighbours about how annoying taxes are.
```

## Technical Stack
- Language: Go (stable modern version, pinned in CI)
- TUI libraries:
  - `github.com/charmbracelet/bubbletea`
  - `github.com/charmbracelet/bubbles`
  - `github.com/charmbracelet/lipgloss`
- Packaging/releases: GitHub Releases + checksums

## Proposed Repository Layout
```text
.
├── cmd/laptop-setup/main.go
├── internal/app/               # app wiring, CLI/TUI mode selection
├── internal/stages/            # stage definitions and execution logic
├── internal/runner/            # command runner, retries, logging
├── internal/state/             # run state persistence/resume
├── internal/ui/                # Bubble Tea models/views
├── templates/                  # existing config templates (reuse)
├── bootstrap.sh                # downloads and executes binary
└── migration-plan.md
```

## Stage Model (Core Contract)
Each stage should be declared as structured metadata plus an execution function.

Suggested fields:
- `ID`: stable identifier (for state/logging)
- `Title`: user-facing stage name
- `Description`: short explanation in UI
- `Precheck`: determines if stage is already satisfied
- `Run`: executes commands/actions
- `DecisionDeps`: references to user decisions this stage consumes
- `CanSkip`: whether user may skip
- `Critical`: if true, failed stage blocks later stages unless explicitly overridden
- `LogTag`: deterministic stage tag used to filter centralized run logs

## Phase Model (User Decision Layer)
Phases are user-facing planning buckets that collect decisions before execution. Phases do not replace execution stages.

Suggested fields:
- `ID`: stable identifier for phase-level UI/state
- `Title`: user-facing phase name
- `Description`: short explanation in UI
- `Configure`: renders prompts and persists decisions
- `StageIDs`: ordered list of stage IDs governed by this phase

## Stage Mapping From Current Behavior
Map from existing shell-era setup behavior:
1. **Phase: `macos_setup` (MacOS setup)**
   - `xcode_clt`
     - Precheck: `xcode-select -p`
     - If missing: install Command Line Tools via `softwareupdate`
     - Mark `already_done` when CLT is already present
   - `macos_defaults`
2. **Phase: `install_apps_packages` (Install apps/packages)**
   - `homebrew_install`
   - `brew_bundle` (generate a run-scoped Brewfile from selected package/app entries, then run `brew bundle install --file <generated-brewfile>`)
3. **Phase: `dev_tools_setup` (Dev tools setup)**
   - `vite_plus_install` (or alternative Node toolchain path per user decision)
   - `docker_config` (e.g., Colima toggle/config)
   - `shell_setup` (oh-my-zsh + zshrc + starship; decision-driven options)
   - `git_config` (confirm/edit identity and config prior to run)
4. **Phase: `manual_steps` (Manual App Store apps)**
   - `manual_app_store_apps` (summary/reminder stage, no automation)

Notes:
- Stage IDs remain stable and granular for `--from`, `--only`, `--skip`, and resume behavior.
- Phases are a UX/planning layer; stages remain the execution/recovery layer.

## CLI Contract (Target)
Support both interactive and automation use cases:
- `-y, --yes` (non-interactive, auto-approve applicable stages)
- `--resume` (continue from last incomplete run)
- `--from <stage-id>` (start from stage)
- `--only <stage-id>[,<stage-id>...]`
- `--skip <stage-id>[,<stage-id>...]`
- `--dry-run` (simulate the full flow without mutating the machine)
- Package/app selection is configured interactively in the TUI selection screen.
- No voice output feature is implemented.

## TUI Experience (Target)
Primary views:
1. Welcome/setup summary
2. Phase decision wizard:
   - `MacOS setup`: confirm defaults/skip options where applicable
   - `Install apps/packages`: list package/app catalog entries with check/uncheck selection (all checked by default)
   - `Dev tools setup`:
     - Node environment choice (`vite+` vs `nvm + pnpm`)
     - Docker runtime preference (e.g., use Colima)
     - Shell setup options
     - Git identity/config confirmation and edits
   - `Manual App Store apps`: reminder configuration for final checklist
3. Execution plan review/confirmation:
   - show selected decisions
   - show resolved stage list and order
4. Stage checklist with status indicators:
   - `pending`, `running`, `success`, `skipped`, `failed`, `already_done`
5. Live execution panel with spinner + tailed logs
6. Failure dialog with actions:
   - Retry stage
   - Skip stage (if allowed)
   - Abort run
7. Final summary with:
   - Completed/skipped/failed counts
   - Run log paths
   - manual App Store installs reminder

## Reliability and Idempotency Rules
- Stage prechecks must mark already-satisfied setup as `already_done` rather than fail.
- File writes should be deterministic and safe to rerun.
- Phase decisions are collected before execution and persisted as run configuration.
- The `brew_bundle` stage should generate a deterministic run-scoped Brewfile (for example under `~/.laptop-setup/runs/<run-id>/`) from confirmed selection IDs and reuse it on resume/retry.
- Stages must be pure consumers of persisted decisions (no hidden interactive prompts in stage execution).
- External command failures must capture:
  - exit code
  - failing command
  - run log path + stage id/attempt context
- Resume should restart at first non-successful stage.
- Resume should reuse persisted decisions and resolved plan by default.
- `--yes` mode should never block for TTY interaction.

## Dry-Run Mode Specification
`--dry-run` should be treated as a full simulation mode for UX and flow testing.

Behavior requirements:
- No mutating commands are executed (`defaults write`, installers, file overwrites, package installs).
- TUI screens and prompts remain identical to normal mode so interaction paths are testable.
- Dry-run still performs phase decision prompts and plan resolution.
- Each stage renders the commands/actions it would run.
- Stage status uses `simulated_success` (or equivalent clear simulated status) instead of `success`.
- Logs are still written, clearly prefixed/labeled as dry-run output.
- State file records that run mode was dry-run and must not be resumable into a real run.

Implementation guidance:
- Stage contract should expose two paths:
  - `Run`: real execution
  - `Simulate`: dry-run preview/details
- If `Simulate` is not provided for a stage, fallback behavior should print a structured action plan for that stage.
- Optional follow-on flag for testing error UX: `--dry-run-fail-at <stage-id>` to force a simulated failure path.

## State and Logging
- State directory: `~/.laptop-setup/`
- Run file: `~/.laptop-setup/state.json`
- Runs directory: `~/.laptop-setup/runs/<run-id>/`
- Human-readable run log: `run.log` (combined output across all stages)
- Structured event log: `events.jsonl` (one JSON event per line)
- Optional concise summary artifact at run end: `summary.json`

`events.jsonl` records should include:
- timestamp
- level
- run id
- stage id
- attempt
- mode (`normal`/`dry-run`)
- event type
- command (when applicable)
- exit code (when applicable)
- message

Logging behavior:
- All stage output is centralized per run and tagged with `stage_id`.
- TUI "live logs" tails `run.log` and filters by current stage tag.
- Dry-run logs are explicitly labeled as simulated output.
- In non-interactive (`--yes`) mode, keep file logging as source-of-truth and optionally mirror concise progress to stdout.

State should include:
- run id, start/end timestamps
- phase decisions (selected package/app/tooling options)
- selected package/app entry IDs used to generate the run-scoped Brewfile
- resolved execution plan (ordered stage IDs derived from decisions)
- per-stage status + attempts
- last failure details (if any)

## Bootstrap and Distribution Plan
`bootstrap.sh` responsibilities:
1. Validate args and detect architecture.
2. Download release artifact for host (`darwin-arm64`/`darwin-amd64`).
3. Download and verify checksum.
4. Mark executable and run with forwarded flags.

Artifact strategy:
- `laptop-setup_darwin_arm64`
- `laptop-setup_darwin_amd64`
- `checksums.txt`

## Security and Trust
- Use pinned GitHub release URLs.
- Verify SHA256 checksums before execution.
- Keep all remote script usage explicit and logged.
- Prefer official installers when unavoidable (Homebrew, Vite+, oh-my-zsh) and isolate output in centralized run logs tagged by stage.

## Testing Strategy
1. Unit tests:
   - CLI parsing
   - decision-to-plan resolution (phase decisions -> stage execution plan)
   - stage selection (`--only`, `--skip`, `--from`)
   - state transitions and resume behavior
2. Integration tests (mock runner):
   - success path
   - retry path
   - non-interactive path
   - phase prompt flow and execution-plan confirmation flow
   - generated Brewfile content and `brew bundle` invocation match confirmed selections
   - dry-run path (no mutating command execution)
   - dry-run forced failure path (if `--dry-run-fail-at` is implemented)
3. Manual smoke tests on clean macOS VM:
   - interactive run with default "all-selected" package/app list
   - interactive run with custom package/app deselection
   - phase decisions for package selection / Node toolchain / Docker runtime / git config
   - interrupted run + resume
   - stage skip and failure recovery
   - dry-run TUI walkthrough (confirm no system changes)

## Migration Phases (Executable Contract)
This section is intentionally structured for fresh agent-session execution using prompts like:
`Please execute Phase N of migration-plan.md and mark it complete.`

Execution rules for each phase run:
1. Execute only the requested phase.
2. Respect `DependsOn`; if unmet, set phase to `blocked` with reason and stop.
3. Keep changes scoped to the phase `Scope` and listed deliverables.
4. Run all listed verification commands for that phase.
5. Update `Phase Status Ledger` and `Completion Notes` in this file before finishing.

Allowed phase statuses:
- `pending`: not started
- `in_progress`: work actively being implemented
- `blocked`: cannot finish due to unmet dependency or failing gate
- `done`: completion gate passed

### Phase Status Ledger
1. Phase 1 - Foundation: `done`
2. Phase 2 - Parity CLI Runner: `done`
3. Phase 3 - TUI Layer: `done`
4. Phase 4 - Bootstrap Cutover: `done`
5. Phase 5 - Hardening: `done`
6. Phase 6 - Finalize: `done`

### Phase 1 - Foundation
- DependsOn: none
- Scope:
  - Initialize Go module and baseline project structure.
  - Implement stage contract, command runner abstraction, logger skeleton, and state persistence skeleton.
- OutOfScope:
  - Full stage behavior parity.
  - TUI screens.
  - Bootstrap cutover.
- Deliverables:
  - `go.mod` and `cmd/laptop-setup/main.go`.
  - Initial `internal/stages`, `internal/runner`, `internal/state`, and `internal/app` wiring.
  - Minimal end-to-end invocation path that compiles.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - Repository builds/tests cleanly via verification command.
  - Core package structure exists and is wired into a runnable entry point.

### Phase 2 - Parity CLI Runner
- DependsOn: Phase 1 `done`
- Scope:
  - Recreate existing setup behavior as structured stages without TUI interaction.
  - Implement CLI flags required for execution control: `--yes`, `--resume`, `--from`, `--only`, `--skip`, `--dry-run`.
  - Implement decision persistence + stage plan resolution in CLI mode.
- OutOfScope:
  - Bubble Tea views and interaction model.
  - Bootstrap artifact download flow.
- Deliverables:
  - Stage implementations mapped from current behavior (`xcode_clt`, `macos_defaults`, `homebrew_install`, `brew_bundle`, `vite_plus_install`, `docker_config`, `shell_setup`, `git_config`, `manual_app_store_apps`).
  - Resume-aware state transitions and per-stage status tracking.
  - Dry-run simulation path with non-mutating behavior.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - CLI can execute stages end-to-end without TUI.
  - Flag behaviors and resume flow are wired and testable.
  - Dry-run mode does not execute mutating actions.

### Phase 3 - TUI Layer
- DependsOn: Phase 2 `done`
- Scope:
  - Build Bubble Tea flow for phase decisions, plan review, stage checklist, and live progress/log display.
  - Add failure handling actions (retry/skip/abort) in UI.
- OutOfScope:
  - Release distribution changes in `bootstrap.sh`.
- Deliverables:
  - Interactive decision wizard aligned to phase model.
  - Execution view with stage status transitions and tailed logs.
  - Failure dialog actions integrated with runner/state.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - Interactive TUI can complete a run and show final summary.
  - Retry/skip/abort actions are connected to execution state transitions.

### Phase 4 - Bootstrap Cutover
- DependsOn: Phase 3 `done`
- Scope:
  - Update `bootstrap.sh` to download architecture-correct binary release artifact.
  - Add checksum download and SHA256 verification before execution.
  - Keep bootstrap binary-only with explicit fail-fast errors on bootstrap failures.
- OutOfScope:
  - Alternative fallback execution paths.
- Deliverables:
  - Binary-first bootstrap path for `darwin-arm64` and `darwin-amd64`.
  - Checksum validation and clear bootstrap failure output.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - Bootstrap path resolves artifact by architecture and verifies checksum.
  - Script forwards flags to the binary correctly.

### Phase 5 - Hardening
- DependsOn: Phase 4 `done`
- Scope:
  - Expand automated test coverage and integration checks.
  - Add/refresh operational troubleshooting documentation.
  - Validate VM smoke-test checklist coverage.
- OutOfScope:
  - Default-doc workflow switch and legacy marker finalization.
- Deliverables:
  - Unit/integration tests covering selection logic, state/resume, dry-run, and failure paths.
  - Documented smoke-test checklist and operator troubleshooting notes.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - Test suite passes and includes critical path coverage from Testing Strategy.
  - Troubleshooting and operational docs are present and updated.

### Phase 6 - Finalize
- DependsOn: Phase 5 `done`
- Scope:
  - Make binary workflow the default documented path.
  - Document no `setup.sh` fallback policy as intentional.
  - Ensure migration artifacts and docs are internally consistent.
- OutOfScope:
  - Post-migration feature expansion.
- Deliverables:
  - README/docs updated to binary-first workflow.
  - Clear no-fallback messaging for `setup.sh`.
  - Final validation pass against global Definition of Done.
- Verification Commands:
  - `go test ./...`
- Completion Gate (all required):
  - Documentation defaults to binary flow.
  - No-fallback policy is explicitly documented.

### Completion Notes
Append one line when a phase reaches a terminal status (`done` or `blocked`):
- Format: `YYYY-MM-DD | Phase N | status=<done|blocked> | commit=<sha> | notes=<short summary>`
- Example: `2026-05-23 | Phase 2 | status=done | commit=abc1234 | notes=CLI parity and dry-run path implemented`
2026-05-23 | Phase 1 | status=done | commit=6e10949 | notes=Initialized Go module, entrypoint, and core app/stage/runner/state scaffolding with passing build verification
2026-05-23 | Phase 2 | status=done | commit=6ceeae5 | notes=Implemented parity CLI stage runner with stage selection flags, resume-aware state transitions, and dry-run simulation behavior
2026-05-23 | Phase 3 | status=done | commit=97cb3b1 | notes=Implemented Bubble Tea phase wizard, plan review, live execution checklist/log view, and retry/skip/abort failure actions wired to state transitions
2026-05-23 | Phase 4 | status=done | commit=3ba8147 | notes=Cut over bootstrap.sh to architecture-aware binary download with SHA256 verification, flag forwarding, and clear bootstrap failure output
2026-05-23 | Phase 5 | status=done | commit=uncommitted | notes=Expanded critical path tests for dry-run/resume/failure handling and added operator troubleshooting plus VM smoke-test docs
2026-05-23 | Phase 6 | status=done | commit=uncommitted | notes=Updated documentation to binary-first workflow with explicit no-fallback bootstrap policy

## Definition of Done
Global DoD is satisfied only when all items below are true:
1. Phase status ledger shows Phases 1-6 as `done`.
2. New binary handles full setup flow end-to-end on a fresh macOS machine.
3. Interactive TUI phase decision prompts drive a clear stage execution flow with error recovery.
4. Non-interactive mode works for unattended runs.
5. Dry-run mode provides full UX simulation with zero machine mutation.
6. Resume works after interruption/failure and reuses persisted decisions/plan.
7. Bootstrap path is binary-first and checksum-verified.
8. Documentation reflects the new default workflow and states there is no `setup.sh` fallback path.
