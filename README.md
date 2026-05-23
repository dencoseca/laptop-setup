# laptop-setup

Provides the perfect environment to ignore time, hunger, friends and family,
most of life's responsibilities, stop blinking entirely, and eventually be able
to afford a second home, somewhere in the mediterranean, where you can talk
to your neighbours about how annoying taxes are.

```shell
curl -fsSL https://raw.githubusercontent.com/dencoseca/laptop-setup/main/bootstrap.sh -o bootstrap.sh
sh bootstrap.sh --environment work
```

`bootstrap.sh` is the default entrypoint. It downloads the release binary for your macOS architecture, verifies SHA256 checksums, then executes `laptop-setup`.

For non-interactive use:
```shell
sh bootstrap.sh --environment work --yes
```

Common flags:

| Flag | Valid Values |
|------|--------------|
| `--environment`, `-e` | `home`, `work` |
| `--yes`, `-y` | non-interactive mode |
| `--resume` | resume previous run |
| `--dry-run` | simulate without system mutation |

## Legacy Script

`setup.sh` is a legacy compatibility path and is not the default workflow. Prefer `bootstrap.sh` for all standard runs.
