# VM Smoke Test Checklist

Use this checklist on a clean macOS VM to validate migration behavior end-to-end.
Use `bootstrap.sh` as the default execution path; use `go run ./cmd/laptop-setup` only when validating from a local checkout.
Bootstrap is intentionally binary-only: there is no `setup.sh` fallback when bootstrap validation fails.

## Preconditions

- Clean Apple Silicon macOS VM snapshot.
- Network access enabled.
- Local checkout of this repository.
- Terminal with write access to `$HOME`.

## Test Matrix

Mark each item pass/fail and capture `run_id` plus log path.

1. Interactive run with default package/app selection (all selected).
2. Interactive run with custom package/app deselection in Brew selection screen.
3. Interactive phase decisions verified:
   - package/app selection
   - dev tools phase toggles
   - manual apps summary
4. Interactive run with `--only` stage filtering:
   ```shell
   go run ./cmd/laptop-setup --only brew_bundle
   ```
5. Interrupted run and resume:
   - start run
   - interrupt after at least one completed stage
   - resume with:
     ```shell
     go run ./cmd/laptop-setup --resume
     ```
6. Failure handling path:
   - force a stage failure (for example, temporarily break connectivity for a network stage)
   - verify retry/skip/abort behavior
7. Dry-run walkthrough:
   ```shell
   go run ./cmd/laptop-setup --dry-run
   ```
   - verify stage statuses are `simulated_success`
   - verify no system-mutating side effects

## Validation Checks Per Run

- `~/.laptop-setup/state.json` is updated.
- Run directory exists at `~/.laptop-setup/runs/<run-id>/`.
- `run.log` and `events.jsonl` both exist.
- `events.jsonl` includes stage lifecycle events (`stage_started`, `stage_completed`, and failures when applicable).
- `brew_bundle` stage uses run-scoped `Brewfile.generated` when selected.

## Sign-off Template

- Date:
- Tester:
- VM image/version:
- Result: pass | fail
- Failed checks (if any):
- Notes:
