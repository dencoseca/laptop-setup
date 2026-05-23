# laptop-setup Troubleshooting

This runbook is for operators validating or supporting the Go-based setup flow during migration.
Default production entrypoint is `bootstrap.sh` (binary-first). `go run` workflows are for local repository validation.

## Runtime Artifacts

- State file: `~/.laptop-setup/state.json`
- Run directory: `~/.laptop-setup/runs/<run-id>/`
- Human log: `~/.laptop-setup/runs/<run-id>/run.log`
- Structured log: `~/.laptop-setup/runs/<run-id>/events.jsonl`

Use the latest `run_id` from `state.json` to locate the active run directory.

## Quick Triage Flow

1. Check the latest state:
   ```shell
   cat ~/.laptop-setup/state.json
   ```
2. Inspect the most recent failures:
   ```shell
   tail -n 80 ~/.laptop-setup/runs/<run-id>/run.log
   ```
3. Inspect structured error events:
   ```shell
   rg '"level":"error"' ~/.laptop-setup/runs/<run-id>/events.jsonl
   ```
4. Resume when appropriate:
   ```shell
   laptop-setup --yes --resume
   ```

## Common Failure Cases

### No TTY in interactive mode

Symptom:
- Error indicates interactive mode requires a TTY.

Action:
- Re-run in non-interactive mode with explicit environment:
  ```shell
  laptop-setup --yes --environment work
  ```

### Resume blocked by mode mismatch

Symptom:
- `cannot resume a normal run as dry-run`.

Action:
- Resume using the original mode.
- For dry-run validation, start a fresh dry-run instead of resuming a normal run.

### `brew_bundle` stage fails

Symptom:
- `brew bundle install` command failure in `run.log`.

Action:
1. Validate generated file exists:
   ```shell
   ls ~/.laptop-setup/runs/<run-id>/Brewfile.generated
   ```
2. Re-run the exact command from logs manually to confirm local brew issue.
3. Fix brew/network/system issue, then resume:
   ```shell
   laptop-setup --yes --resume
   ```

### Stage marked failed and skip not allowed

Symptom:
- Failure says stage cannot be skipped.

Action:
- Fix the underlying issue and retry that stage by resuming.
- For a one-stage investigation run, use:
  ```shell
  laptop-setup --yes --environment work --only <stage-id>
  ```

### Missing previous state for resume

Symptom:
- `no previous run state found for --resume`.

Action:
- Start a new run with explicit environment:
  ```shell
  laptop-setup --yes --environment home
  ```

## Escalation Data to Collect

When opening an issue, include:

- `run_id`
- Full failing stage id
- Last 120 lines of `run.log`
- Matching error lines from `events.jsonl`
- CLI invocation flags used
