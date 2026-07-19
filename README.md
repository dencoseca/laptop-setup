# laptop-setup

Provides the perfect environment to ignore time, hunger, friends and family,
most of life's responsibilities, stop blinking entirely, and eventually be able
to afford a second home, somewhere in the mediterranean, where you can talk
to your neighbours about how annoying taxes are.

```shell
curl -fsSL https://raw.githubusercontent.com/dencoseca/laptop-setup/main/bootstrap.sh -o bootstrap.sh
sh bootstrap.sh
```

This project targets Apple Silicon MacBooks only. Intel Macs are not a supported
runtime target.

`bootstrap.sh` is the default entrypoint. It is intended to run on a fresh Apple
Silicon macOS install using only tools that ship with macOS: `sh`, `curl`,
`awk`, `chmod`, `mktemp`, `rm`, `shasum`, and `uname`. It downloads the latest
Apple Silicon binary from this repository's GitHub Releases, verifies its
GitHub release SHA-256 digest, and runs it. The binary embeds its setup
templates, so no local checkout, Git, Go toolchain, Bash, or Homebrew
installation is required before starting the app.

There is intentionally no `setup.sh` fallback path: bootstrap fails fast when
the host is unsupported or the release binary cannot be downloaded.

Common flags:

| Flag | Valid Values |
|------|--------------|
| `--resume` | resume previous run |
| `--from <stage-id>` | start execution from a stage |
| `--only <stage-id>[,<stage-id>...]` | run only specific stages |
| `--skip <stage-id>[,<stage-id>...]` | skip specific stages |
| `--dry-run` | simulate without system mutation |

Package/app selection is configured interactively in the TUI.
The default terminal setup installs Ghostty and configures it alongside
oh-my-zsh, Starship, fuzzy history search, inline suggestions, syntax
highlighting, and quiet login shells.
No voice output feature is implemented.
