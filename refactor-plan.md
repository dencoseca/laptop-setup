# Laptop Setup Post-Migration Refactor Plan

## Purpose
This plan captures gaps that were missed in `migration-plan.md` and now need implementation.
All phases below are actionable for an implementing agent.

## Constraints (Explicit Product Decisions)
1. Do **not** add a `--brew-selection` CLI flag. Brew selection remains interactive in TUI.
2. Do **not** implement any `say`/voice output feature. Remove `--no-say` references and keep voice functionality out of the codebase.
3. Do **not** restore `setup.sh` and do **not** add bootstrap fallback to any legacy script. Bootstrap must run binary or fail with clear error output.

## Execution Rules
1. Execute one phase at a time.
2. Keep changes scoped to the listed deliverables.
3. Run each phase verification command(s) before marking done.
4. Update the phase ledger and completion notes in this file after each phase.

## Phase Status Ledger
1. Phase 1 - CLI Contract Cleanup: `done`
2. Phase 2 - Bootstrap/Legacy Policy Alignment: `done`
3. Phase 3 - Dev Tools Decision Model: `done`
4. Phase 4 - TUI Live Log Tail/Filter: `done`
5. Phase 5 - Final Summary Manual Reminder: `pending`
6. Phase 6 - Centralized Stage Output Logging: `pending`
7. Phase 7 - Integration Coverage Expansion: `pending`
8. Phase 8 - Pinned Bootstrap Release Strategy: `pending`

## Phase 1 - CLI Contract Cleanup
- Goal:
  - Align CLI/docs with current product decisions.
- Deliverables:
  - Remove `--no-say` references from docs/plans/help text.
  - Keep existing supported flags (`--yes`, `--resume`, `--from`, `--only`, `--skip`, `--dry-run`).
  - Ensure no runtime `say` command or voice pathway exists in Go code, scripts, or templates.
  - Ensure no `--brew-selection` contract appears in user-facing docs.
- Implementation Notes:
  - Update `migration-plan.md` and README/docs sections that still describe obsolete flag expectations.
  - Prefer explicit wording: "No voice output feature is implemented."
- Verification:
  - `rg -n "no-say|--no-say|\\bsay\\b|brew-selection|--brew-selection" README.md docs migration-plan.md internal cmd bootstrap.sh templates`
  - `go test ./...`

## Phase 2 - Bootstrap/Legacy Policy Alignment
- Goal:
  - Make the fail-fast binary-only bootstrap behavior explicit and consistent across documentation.
- Deliverables:
  - Keep bootstrap behavior as binary download + checksum verify + execute, otherwise fail.
  - Improve bootstrap failure messages so root cause is explicit (unsupported arch, download failure, checksum mismatch, missing sha tool).
  - Update docs to state there is no `setup.sh` fallback and that this is intentional.
- Implementation Notes:
  - Preserve architecture handling (`darwin_arm64`/`darwin_amd64`) and flag forwarding.
  - Do not add fallback execution paths.
- Verification:
  - `sh bootstrap.sh --help`
  - `go test ./...`

## Phase 3 - Dev Tools Decision Model
- Goal:
  - Implement missing decision-driven behavior in the dev-tools phase.
- Deliverables:
  - Add TUI decision prompt for Node toolchain:
    - `vite+`
    - `pnpm + nvm`
  - Implement stage behavior based on persisted decision:
    - `vite+`: run official Vite+ install curl command.
    - `pnpm + nvm`: run official `nvm` and `pnpm` install curl commands.
  - Add Docker runtime decision prompt with selectable list (currently one option: `colima`), persisted for future extensibility.
  - Add shell setup options and git identity/config confirmation/edit prompts so phase decisions are richer than stage on/off toggles.
  - Persist all decisions into run state and consume them from stage execution (no hidden prompts inside stages).
- Implementation Notes:
  - Non-interactive (`--yes`) must choose deterministic defaults.
  - Resume must reuse persisted decisions.
  - Keep stage IDs stable.
- Verification:
  - `go test ./...`
  - Add/extend tests covering decision persistence and decision-to-stage behavior.

## Phase 4 - TUI Live Log Tail/Filter
- Goal:
  - Match planned live-log UX.
- Deliverables:
  - Execution screen tails `run.log` from disk.
  - Show logs filtered to current stage tag (or equivalent deterministic stage filter).
  - Keep spinner + stage checklist behavior unchanged.
- Implementation Notes:
  - Avoid unbounded memory growth while tailing.
  - Handle log file not-yet-created and EOF polling cleanly.
- Verification:
  - `go test ./...`
  - Add targeted tests for log line filtering/parsing functions.

## Phase 5 - Final Summary Manual Reminder
- Goal:
  - Include manual App Store reminder in final summary.
- Deliverables:
  - Final summary view includes explicit manual app reminder list/section.
  - Keep existing completed/skipped/failed counts and log paths.
- Verification:
  - `go test ./...`

## Phase 6 - Centralized Stage Output Logging
- Goal:
  - Ensure stage command output is fully centralized in run logs.
- Deliverables:
  - Capture and write command stdout/stderr into run logs with stage/attempt context.
  - Preserve structured event logging (`events.jsonl`) and command lifecycle events.
  - Ensure command failure records include command + exit code + context.
- Implementation Notes:
  - Keep logs readable in human log while avoiding duplicated noise.
  - Do not break existing dry-run event semantics.
- Verification:
  - `go test ./...`
  - Add tests for logging behavior around command success/failure output capture.

## Phase 7 - Integration Coverage Expansion
- Goal:
  - Close testing gaps from the migration strategy.
- Deliverables:
  - Add integration-style tests (mock runner/store where needed) for:
    - non-interactive end-to-end path
    - phase prompt flow + execution-plan confirmation behavior
    - generated Brewfile + brew bundle invocation alignment with selected entries
    - resume flow after interruption/failure using persisted plan/decisions
  - Keep existing unit tests and extend instead of replacing.
- Verification:
  - `go test ./...`

## Phase 8 - Pinned Bootstrap Release Strategy
- Goal:
  - Implement pinned release URL strategy by default (not `latest`) while keeping operator override.
- Deliverables:
  - Bootstrap defaults to a pinned tag source (configurable in one obvious location).
  - Retain optional override via environment variable.
  - Update docs to explain default pin and override.
- Implementation Notes:
  - Keep checksum verification mandatory.
  - Keep artifact naming strategy unchanged.
- Verification:
  - `sh bootstrap.sh --help`
  - `go test ./...`

## Completion Notes
Append one line per terminal phase update:
- Format: `YYYY-MM-DD | Phase N | status=<done|blocked> | commit=<sha> | notes=<short summary>`
- `2026-05-23 | Phase 1 | status=done | commit=87da472 | notes=Removed obsolete CLI flag references from docs/plans, documented interactive package selection and no voice output, verification commands passed.`
- `2026-05-23 | Phase 2 | status=done | commit=uncommitted | notes=Improved bootstrap fail-fast root-cause errors, documented intentional no-setup.sh fallback policy, and passed phase verification commands.`
- `2026-05-23 | Phase 3 | status=done | commit=uncommitted | notes=Added persisted dev-tools decisions (node, docker, shell, git) in TUI and --yes defaults, wired stages to consume decisions, and added tests for decision persistence and decision-driven stage behavior.`
- `2026-05-23 | Phase 4 | status=done | commit=uncommitted | notes=Execution screen now tails run.log from disk with bounded buffering, filters visible log lines to the current stage tag, handles polling/EOF/partial lines cleanly, and adds targeted log parsing/filtering tests; go test ./... passed.`
