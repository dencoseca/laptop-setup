# laptop-setup Troubleshooting

This runbook is for operators validating or supporting the Go-based setup flow during migration.
Default production entrypoint is `bootstrap.sh` (binary-first). `go run` workflows are for local repository validation.
`bootstrap.sh` intentionally has no `setup.sh` fallback path; it only runs verified release binaries and exits on bootstrap errors.
`bootstrap.sh` defaults to pinned release tag `v0.1.0`; override with `LAPTOP_SETUP_RELEASE_TAG` (for example `v0.1.1` or `latest`) when operators need to target a different release.

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

### Bootstrap script exits before `laptop-setup` starts

Symptom:
- `bootstrap error:` includes one of:
  - unsupported host OS/arch
  - release artifact download failure
  - checksum lookup/mismatch
  - missing checksum tool (`shasum` or `sha256sum`)

Action:
1. Use the exact error text to fix the root cause.
2. Re-run:
   ```shell
   sh bootstrap.sh [flags]
   ```
3. Do not expect fallback to legacy scripts; bootstrap intentionally fails fast until verification passes.

### No TTY in interactive mode

Symptom:
- Error indicates interactive mode requires a TTY.

Action:
- Re-run in non-interactive mode:
  ```shell
  laptop-setup --yes
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
  laptop-setup --yes --only <stage-id>
  ```

### Missing previous state for resume

Symptom:
- `no previous run state found for --resume`.

Action:
- Start a new run:
  ```shell
  laptop-setup --yes
  ```

## Escalation Data to Collect

When opening an issue, include:

- `run_id`
- Full failing stage id
- Last 120 lines of `run.log`
- Matching error lines from `events.jsonl`
- CLI invocation flags used
