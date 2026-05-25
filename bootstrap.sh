#!/usr/bin/env sh

set -eu

REPO_SLUG="dencoseca/laptop-setup"
CLI_PACKAGE="github.com/dencoseca/laptop-setup/cmd/laptop-setup"
CLI_REF="main"
TMP_DIR=""
BUILT_BINARY=""
SHOW_HELP=0
BOOTSTRAP_ERROR=""

print_usage() {
  cat <<'EOF'
Usage:
  sh bootstrap.sh [flags]

Common flags:
  --resume                        Resume an interrupted run
  --dry-run                       Simulate stages without system mutation
  -h, --help                      Show usage

All flags are forwarded to the built `laptop-setup` binary.
Supported host: Apple Silicon macOS.
EOF
  cat <<'EOF'
Bootstrap builds the latest `main` version with `go install` before running it.
EOF
}

log() {
  printf '%s\n' "$1"
}

set_bootstrap_error() {
  BOOTSTRAP_ERROR=$1
  log "$BOOTSTRAP_ERROR"
}

fail() {
  printf 'bootstrap error: %s\n\n' "$1" >&2
  print_usage >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}

parse_args() {
  for arg in "$@"; do
    case "$arg" in
      -h|--help)
        SHOW_HELP=1
        ;;
      *)
        :
        ;;
    esac
  done
}

validate_host() {
  os_name=$(uname -s 2>/dev/null || printf 'unknown')
  machine_arch=$(uname -m 2>/dev/null || printf 'unknown')
  if [ "$os_name" != "Darwin" ]; then
    set_bootstrap_error "Unsupported host: os=$os_name arch=$machine_arch. Supported target: Apple Silicon macOS (Darwin arm64/aarch64)."
    return 1
  fi

  case "$machine_arch" in
    arm64|aarch64)
      return 0
      ;;
    *)
      set_bootstrap_error "Unsupported host: os=$os_name arch=$machine_arch. Supported target: Apple Silicon macOS (Darwin arm64/aarch64)."
      return 1
      ;;
  esac
}

build_latest_binary() {
  if ! command -v go >/dev/null 2>&1; then
    set_bootstrap_error "Missing prerequisite: go is required to build $REPO_SLUG from main."
    return 1
  fi

  if ! validate_host; then
    return 1
  fi

  TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t laptop-setup-bootstrap)
  binary_path=$TMP_DIR/laptop-setup

  log "Building latest laptop-setup from main."
  if ! env GOPROXY=direct GOBIN="$TMP_DIR" go install "$CLI_PACKAGE@$CLI_REF"; then
    set_bootstrap_error "Build failed: unable to install $CLI_PACKAGE@$CLI_REF."
    return 1
  fi

  if [ ! -x "$binary_path" ]; then
    set_bootstrap_error "Build failed: expected binary was not created at $binary_path."
    return 1
  fi

  BUILT_BINARY=$binary_path
  return 0
}

trap cleanup EXIT INT TERM

parse_args "$@"
if [ "$SHOW_HELP" -eq 1 ]; then
  print_usage
  exit 0
fi

if build_latest_binary; then
  log "Starting laptop-setup."
  "$BUILT_BINARY" "$@"
  exit $?
fi

if [ -n "$BOOTSTRAP_ERROR" ]; then
  fail "$BOOTSTRAP_ERROR"
fi

fail "unable to build laptop-setup binary"
