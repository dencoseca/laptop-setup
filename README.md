# laptop-setup

Provides the perfect environment to ignore time, hunger, friends and family,
most of life's responsibilities, stop blinking entirely, and eventually be able
to afford a second home, somewhere in the mediterranean, where you can talk
to your neighbours about how annoying taxes are.

```shell
curl -fsSL https://raw.githubusercontent.com/dencoseca/laptop-setup/main/bootstrap.sh -o bootstrap.sh
sh bootstrap.sh
```

`bootstrap.sh` is the default entrypoint. It downloads the release binary for your macOS architecture, verifies SHA256 checksums, then executes `laptop-setup`.
There is intentionally no `setup.sh` fallback path: bootstrap is binary-only and fails fast when download or verification prerequisites are not met.
Bootstrap defaults to pinned GitHub release tag `v0.1.0`.
Override it with `LAPTOP_SETUP_RELEASE_TAG` when needed (including `latest`):

```shell
LAPTOP_SETUP_RELEASE_TAG=v0.1.1 sh bootstrap.sh
LAPTOP_SETUP_RELEASE_TAG=latest sh bootstrap.sh
```

For non-interactive use:
```shell
sh bootstrap.sh --yes
```

Common flags:

| Flag | Valid Values |
|------|--------------|
| `--yes`, `-y` | non-interactive mode |
| `--resume` | resume previous run |
| `--from <stage-id>` | start execution from a stage |
| `--only <stage-id>[,<stage-id>...]` | run only specific stages |
| `--skip <stage-id>[,<stage-id>...]` | skip specific stages |
| `--dry-run` | simulate without system mutation |

Package/app selection is configured interactively in the TUI.
No voice output feature is implemented.
