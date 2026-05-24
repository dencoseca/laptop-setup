# Improvement Plan

This document turns the code review into a phased execution plan. Each phase is intended to be small enough for Codex to execute independently while keeping the application runnable after every step.

The overall direction is:

- Keep the command usable after every phase.
- Prefer typed domain concepts over stringly typed maps.
- Move orchestration out of the TUI.
- Keep Charmbracelet models focused on input, rendering, and message handling.
- Put shell, filesystem, templates, and package manager behavior behind explicit ports.
- Strengthen persistence and resume behavior before larger refactors.

Before starting any phase, run:

```sh
go test ./...
go vet ./...
```

After each phase, run the same commands again. If a phase changes TUI behavior, also run the relevant `internal/ui` tests directly:

```sh
go test ./internal/ui
```

## Phase 1: Fix Immediate Robustness Issues

Goal: address correctness risks without changing the broader architecture.

### Tasks

1. Make missing Homebrew a real failure in normal execution.
   - Current behavior: `runBrewBundle` logs a simulation message and returns nil when `brew` is missing.
   - Desired behavior: in normal mode, missing `brew` should return an error so the execution engine marks the stage failed and invokes failure handling.
   - Dry-run behavior may continue to log what would happen.

2. Make run IDs collision-resistant.
   - Current behavior: `state.NewRunID` uses second precision.
   - Desired behavior: run IDs should not collide when two runs start in the same second.
   - Keep IDs filesystem-safe.
   - Acceptable approaches:
     - UTC timestamp with nanoseconds plus short random suffix.
     - ULID or UUID if adding a dependency is justified.
     - Exclusive run directory creation with retry.

3. Validate run IDs before using them in paths.
   - Add validation in `state.RunDir`.
   - Reject empty IDs, absolute paths, path traversal, path separators, and unexpected characters.
   - Keep valid generated run IDs accepted.

4. Preserve existing behavior for normal successful runs.
   - Do not alter CLI flags.
   - Do not change the user-facing flow unless required by the bug fix.

### Suggested Files

- `internal/stages/stages.go`
- `internal/state/state.go`
- `internal/state/state_test.go`
- `internal/stages/stages_test.go`

### Suggested Codex Prompt

```text
Implement Phase 1 from improvements.md. Fix missing Homebrew handling in normal brew_bundle execution, make run IDs collision-resistant and filesystem-safe, validate run IDs in state.RunDir, and add focused tests. Keep behavior otherwise unchanged. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- `runBrewBundle` returns an error when `brew` is unavailable in normal execution.
- Dry-run simulation remains non-mutating.
- Two generated run IDs created close together do not collide in tests.
- `state.RunDir` rejects malicious or invalid IDs such as `../x`, `/tmp/x`, `a/b`, and empty strings.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 2: Validate Persisted State on Load and Resume

Goal: make resume safer by validating state before execution.

### Tasks

1. Add validation for `state.RunState`.
   - Validate `RunID`.
   - Validate `Mode`.
   - Validate `ResolvedPlan` is non-empty.
   - Validate `Stages` map keys are non-empty and statuses are known values.
   - Validate `GeneratedFile`, if set, is safe for the expected run directory or explicitly document why it can be external.

2. Add application-level resume validation.
   - Ensure all stage IDs in the saved plan exist in the current catalog.
   - Ensure saved mode is compatible with requested mode.
   - Ensure the saved plan is still executable.

3. Make decode errors clearer.
   - Distinguish invalid JSON from semantically invalid state.
   - Include field names in validation errors.

4. Add tests for corrupted and malicious state.
   - Invalid mode.
   - Unknown status.
   - Unknown stage ID during resume.
   - Invalid run ID with path traversal.

### Suggested Files

- `internal/state/state.go`
- `internal/state/state_test.go`
- `internal/app/app.go`
- `internal/app/app_test.go`
- `internal/execution/execution.go`
- `internal/execution/execution_test.go`

### Suggested Codex Prompt

```text
Implement Phase 2 from improvements.md. Add semantic validation for loaded RunState and resume state. Validate run IDs, modes, plans, stage statuses, and catalog compatibility. Add focused tests for invalid persisted state and resume failures. Keep CLI behavior compatible. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- Invalid persisted state fails before execution starts.
- Resume fails clearly if the saved plan references an unknown stage.
- Validation errors include enough context to fix the state file.
- Existing valid state files still load.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 3: Introduce Typed Domain Concepts

Goal: reduce stringly typed behavior and make domain rules explicit.

### Tasks

1. Introduce typed domain values.
   - `RunID`
   - `Mode`
   - `StageID`
   - `StageStatus`
   - `FailureAction`, if appropriate

2. Introduce a typed `DecisionSet`.
   - Replace most direct use of `map[string]any` outside persistence compatibility code.
   - Keep JSON compatibility if existing state files need to keep working.
   - Provide conversion functions:
     - `DecisionSetFromMap`
     - `DecisionSet.ToMap`
     - `DecisionSet.Validate`

3. Move decision normalization into the typed domain.
   - Avoid silent fallback for invalid persisted values during resume.
   - Allow defaults when creating a new run.
   - Reject invalid values when loading existing state.

4. Add table-driven tests for decision parsing and validation.

### Suggested Package Shape

Use the existing package layout if preferred, but a cleaner DDD-oriented structure would be:

```text
internal/domain/setup
  decisions.go
  plan.go
  run.go
  stage.go
```

If the move is too large for one phase, introduce types in the current packages first and move packages later.

### Suggested Files

- `internal/stages/decisions.go`
- `internal/stages/decisions_test.go`
- `internal/state/state.go`
- `internal/execution/execution.go`
- `internal/ui/tui.go`
- `internal/app/app.go`

### Suggested Codex Prompt

```text
Implement Phase 3 from improvements.md. Introduce typed domain concepts for run ID, mode, stage ID, stage status, and decisions. Replace broad map[string]any usage at domain and application boundaries while preserving JSON compatibility. Add validation and table-driven tests. Keep the public CLI unchanged. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- New run creation uses typed decisions and modes.
- Resume validation rejects invalid persisted decision values.
- Existing tests pass with minimal behavior changes.
- Most business logic no longer switches on raw string literals.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 4: Separate Domain, Application, and Infrastructure

Goal: move toward DDD boundaries and make stage execution easier to test.

### Target Boundaries

Domain layer:

- Stage definitions and metadata.
- Plan resolution.
- Run state transitions.
- Decision validation.
- Status rules.

Application layer:

- Start a new run.
- Resume a run.
- Execute a plan.
- Coordinate state persistence, logging, and failure policy.

Infrastructure layer:

- OS command execution.
- Filesystem reads and writes.
- Template storage.
- Homebrew detection and bundle execution.
- Git config file manipulation.
- macOS defaults commands.

UI layer:

- Bubble Tea models.
- User input.
- Rendering.
- TUI messages.
- Calls into application services.

### Tasks

1. Create explicit ports.
   - `CommandRunner`
   - `PathResolver`
   - `StateRepository`
   - `RunLogFactory`
   - `TemplateStore`
   - `PackageManager`
   - `FileSystem`, if useful

2. Move shell and filesystem behavior out of stage definitions.
   - Stages should describe what needs to happen.
   - Infrastructure handlers should perform the OS-specific actions.

3. Replace package-level test hooks in `internal/app`.
   - Current globals include `defaultCatalogFn`, `newCommandRunner`, `uiRun`, `executeRun`, `getwd`, and `userHomeDirectory`.
   - Prefer an `App` struct with dependencies injected through a constructor.

4. Keep compatibility wrappers.
   - `app.Run(ctx, args, stdout, stderr)` can remain as the CLI entry point.
   - Internally it should construct the production `App`.

### Suggested Files

- `internal/app/app.go`
- `internal/execution/execution.go`
- `internal/stages/stages.go`
- `internal/runner/runner.go`
- New files under `internal/domain`, `internal/application`, or current package equivalents.

### Suggested Codex Prompt

```text
Implement Phase 4 from improvements.md. Refactor toward DDD boundaries by introducing an App/service with injected dependencies and explicit ports for command execution, state, logs, templates, path resolution, and package management. Remove package-level test hooks where practical. Keep app.Run as the CLI-compatible entry point. Move behavior incrementally so go test ./... stays green.
```

### Acceptance Criteria

- `internal/app` no longer depends on mutable package-level variables for tests.
- Application orchestration can be tested with fake ports.
- Stage/domain code has fewer direct calls to `os`, `exec`, and filesystem APIs.
- CLI behavior remains compatible.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 5: Improve Command Execution and Logging

Goal: make command execution safer, easier to observe, and more consistent.

### Tasks

1. Extend `CommandRunner` behavior.
   - Add support for path lookup or command availability checks through the same abstraction.
   - Consider streaming stdout/stderr for long-running commands.
   - Preserve captured output for tests and logs.

2. Improve command error types.
   - Create a typed command error containing command, exit code, stdout, stderr, and underlying error.
   - Avoid wrapping the same command failure message multiple times.

3. Redact or classify sensitive output where needed.
   - Current logs capture full command output.
   - Decide whether any installer output could include tokens or local secrets.

4. Make event types typed constants.
   - Replace raw event strings with constants.
   - Keep JSON output stable.

### Suggested Files

- `internal/runner/runner.go`
- `internal/runner/logger.go`
- `internal/execution/execution.go`
- `internal/stages/stages.go`
- Tests under `internal/runner` and `internal/execution`

### Suggested Codex Prompt

```text
Implement Phase 5 from improvements.md. Improve command execution and logging by adding command availability through the runner abstraction, typed command errors, event type constants, and tests. Keep log JSON fields compatible. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- Command availability checks no longer bypass `CommandRunner`.
- Failed commands expose exit code and output to callers/tests.
- Log event type strings are centralized.
- Existing event JSON remains compatible.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 6: Refactor the TUI into Smaller Charmbracelet Models

Goal: make `internal/ui/tui.go` maintainable and align better with Bubble Tea practices.

### Current Problem

`internal/ui/tui.go` is large and owns too many responsibilities:

- Screen state.
- Rendering.
- Option list behavior.
- Brew list behavior.
- Input handling.
- Execution setup.
- Log tailing.
- Failure prompts.
- Dashboard layout.

### Tasks

1. Split screen-specific behavior into smaller files.
   - `welcome.go`
   - `options.go`
   - `brew.go`
   - `git_identity.go`
   - `review.go`
   - `execution.go`
   - `failure.go`
   - `summary.go`
   - `dashboard.go`

2. Keep Bubble Tea commands explicit.
   - Long-running work should return commands.
   - Commands may start goroutines when needed, but message flow should stay clear and testable.
   - Avoid hiding application orchestration inside view or input helpers.

3. Move execution preparation out of the model.
   - The model should call an application service to start or resume execution.
   - The service should return run metadata and log paths.

4. Encapsulate reusable components.
   - Toggle list component.
   - Select list component.
   - Brew selection component.
   - Text input component for git identity.

5. Keep existing visual tests green.
   - Preserve dashboard output unless intentionally changed.
   - Add tests around screen transitions and commands.

### Suggested Files

- `internal/ui/tui.go`
- New files under `internal/ui`
- Application service files from Phase 4

### Suggested Codex Prompt

```text
Implement Phase 6 from improvements.md. Refactor the TUI into smaller Charmbracelet-oriented files/components without changing the user flow. Move execution preparation out of the model where Phase 4 services exist. Preserve current rendering and tests, adding focused tests for screen transitions if useful. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- `internal/ui/tui.go` is substantially smaller.
- Screen/component logic is easier to locate.
- The TUI model focuses on messages, state, and rendering.
- Existing visual/layout tests pass.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 7: Make Stage Implementations Idempotent and Reversible Where Practical

Goal: reduce risk when running the tool repeatedly on a real laptop.

### Tasks

1. Add prechecks for more stages.
   - `macos_defaults`
   - `brew_bundle`
   - `node_toolchain`
   - `docker_config`
   - `shell_setup`
   - `git_config`

2. Make file writes safer.
   - Use atomic writes for generated config files.
   - Back up existing files with timestamped backup names, not a single `.bak` that can be overwritten.
   - Preserve permissions where practical.

3. Avoid repeated installer runs when possible.
   - Detect installed tools before running curl installers.
   - Detect existing oh-my-zsh.
   - Detect nvm and pnpm installation.

4. Add dry-run parity tests.
   - Dry-run should not write files or create permanent user config.
   - Dry-run should report the same intended actions.

### Suggested Files

- `internal/stages/stages.go`
- Infrastructure files introduced in Phase 4
- `internal/stages/stages_test.go`

### Suggested Codex Prompt

```text
Implement Phase 7 from improvements.md. Improve stage idempotency and file safety by adding prechecks, atomic writes, timestamped backups, and installer detection. Add tests proving repeated runs do not unnecessarily rewrite or reinstall. Keep dry-run non-mutating. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- Re-running the tool skips work that is already satisfied where practical.
- Existing user files are not overwritten without a recoverable backup.
- Dry-run does not mutate user config.
- `go test ./...` passes.
- `go vet ./...` passes.

## Phase 8: Improve Test Strategy and Documentation

Goal: preserve confidence as the architecture evolves.

### Tasks

1. Add contract tests for ports.
   - State repository.
   - Command runner.
   - Template store.
   - Package manager.

2. Add integration-style tests using temp dirs and fake commands.
   - New run.
   - Resume run.
   - Failed stage retry.
   - Failed stage skip.
   - Missing prerequisite.

3. Add documentation for architecture.
   - Describe package boundaries.
   - Describe state file schema.
   - Describe run directory contents.
   - Describe how to add a new stage.

4. Add a maintainer checklist.
   - Tests to run before release.
   - Manual smoke test steps.
   - VM smoke test references.

### Suggested Files

- `README.md`
- `docs/architecture.md`
- `docs/operations-troubleshooting.md`
- Tests across `internal/...`

### Suggested Codex Prompt

```text
Implement Phase 8 from improvements.md. Strengthen tests around ports, resume, failure handling, and missing prerequisites. Add architecture documentation explaining package boundaries, state schema, run logs, and how to add a stage. Run go test ./... and go vet ./... when done.
```

### Acceptance Criteria

- New architecture docs exist and match the implemented package boundaries.
- Adding a stage has documented steps.
- Integration-style tests cover new run, resume, retry, skip, and missing prerequisite behavior.
- `go test ./...` passes.
- `go vet ./...` passes.

## Execution Guidance for Codex

Use these rules when executing the phases:

1. Work one phase at a time.
2. Keep changes small enough to review.
3. Prefer existing package patterns unless the phase explicitly changes the boundary.
4. Add or update tests in the same phase as code changes.
5. Keep CLI flags and user-facing behavior compatible unless the phase explicitly changes a bug.
6. Do not rewrite the TUI and domain layers in the same phase.
7. Do not introduce new dependencies unless the benefit is concrete and documented in the final response.
8. After each phase, report:
   - Files changed.
   - Behavior changed.
   - Tests run.
   - Any deferred work.

## Suggested Phase Order

Execute phases in this order:

1. Phase 1: Fix Immediate Robustness Issues
2. Phase 2: Validate Persisted State on Load and Resume
3. Phase 3: Introduce Typed Domain Concepts
4. Phase 4: Separate Domain, Application, and Infrastructure
5. Phase 5: Improve Command Execution and Logging
6. Phase 6: Refactor the TUI into Smaller Charmbracelet Models
7. Phase 7: Make Stage Implementations Idempotent and Reversible Where Practical
8. Phase 8: Improve Test Strategy and Documentation

Phases 5 and 6 can be swapped if the application service boundary from Phase 4 is stable enough. Phase 7 should happen after the infrastructure ports exist, because idempotency and file safety are easier to test behind explicit abstractions.
