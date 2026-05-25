# laptop-setup Troubleshooting

This runbook is for operators validating or supporting the Go-based setup flow during migration.
Default production entrypoint is `bootstrap.sh`. It downloads the latest Apple Silicon release binary and runs it.
`go run` workflows are for local repository validation.
`bootstrap.sh` intentionally has no `setup.sh` fallback path; it exits on bootstrap errors.

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
   laptop-setup --resume
   ```

## Common Failure Cases

### Bootstrap script exits before `laptop-setup` starts

Symptom:
- `bootstrap error:` includes one of:
  - unsupported host OS/arch
  - missing system command such as `curl`
  - release binary download failure
  - downloaded binary permission failure

Action:
1. Use the exact error text to fix the root cause.
2. Re-run:
   ```shell
   sh bootstrap.sh [flags]
   ```
3. Do not expect fallback to legacy scripts; bootstrap intentionally fails fast until host validation and binary download pass.

### No interactive TTY

Symptom:
- Error indicates `laptop-setup` requires an interactive TTY.

Action:
- Re-run from a terminal session attached to a TTY. The setup flow is interactive-only.

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
   laptop-setup --resume
   ```

### Stage marked failed and skip not allowed

Symptom:
- Failure says stage cannot be skipped.

Action:
- Fix the underlying issue and retry that stage by resuming.
- For a one-stage investigation run, use:
  ```shell
  laptop-setup --only <stage-id>
  ```

### Missing previous state for resume

Symptom:
- `no previous run state found for --resume`.

Action:
- Start a new run:
  ```shell
  laptop-setup
  ```

## Escalation Data to Collect

When opening an issue, include:

- `run_id`
- Full failing stage id
- Last 120 lines of `run.log`
- Matching error lines from `events.jsonl`
- CLI invocation flags used
