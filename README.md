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
Silicon macOS install with only the system shell available. It downloads the
latest Apple Silicon release binary and runs it. The binary embeds its setup
templates, so no local checkout, Go toolchain, or Homebrew installation is
required before starting the app.

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
No voice output feature is implemented.

## Maintainers

- Architecture and stage-extension notes: [docs/architecture.md](docs/architecture.md)
- Troubleshooting runbook: [docs/operations-troubleshooting.md](docs/operations-troubleshooting.md)
- Clean VM validation: [docs/vm-smoke-test-checklist.md](docs/vm-smoke-test-checklist.md)

Before release, run:

```shell
go test ./...
go vet ./...
GOOS=darwin GOARCH=arm64 go build -o laptop-setup-darwin-arm64 ./cmd/laptop-setup
```

Publish the built binary as the GitHub release asset
`laptop-setup-darwin-arm64`; `bootstrap.sh` downloads that asset from the latest
release by default.
