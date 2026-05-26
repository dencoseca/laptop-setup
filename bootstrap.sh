#!/usr/bin/env sh

set -eu

REPO_SLUG="dencoseca/laptop-setup"
ASSET_NAME="laptop-setup-darwin-arm64"
TMP_DIR=""
DOWNLOADED_BINARY=""
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

All flags are forwarded to the downloaded `laptop-setup` binary.
Supported host: Apple Silicon macOS.
EOF
  cat <<'EOF'
Bootstrap downloads the latest release binary and runs it.
It uses the default macOS shell plus curl, chmod, mktemp, uname, and rm.
Set LAPTOP_SETUP_VERSION to a tag such as v1.2.3 to pin a release.
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

require_command() {
  name=$1
  if ! command -v "$name" >/dev/null 2>&1; then
    set_bootstrap_error "Missing required macOS command: $name."
    return 1
  fi
  return 0
}

ensure_tmp_dir() {
  if [ -z "$TMP_DIR" ]; then
    TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t laptop-setup-bootstrap)
  fi
}

download_binary() {
  if ! require_command curl; then
    return 1
  fi
  if ! require_command chmod; then
    return 1
  fi
  if ! require_command mktemp; then
    return 1
  fi

  ensure_tmp_dir
  binary_path=$TMP_DIR/laptop-setup

  version=${LAPTOP_SETUP_VERSION:-latest}
  if [ "$version" = "latest" ]; then
    url="https://github.com/$REPO_SLUG/releases/latest/download/$ASSET_NAME"
  else
    url="https://github.com/$REPO_SLUG/releases/download/$version/$ASSET_NAME"
  fi

  log "Downloading laptop-setup binary from $url."
  if ! curl -fL --retry 3 --retry-delay 2 "$url" -o "$binary_path"; then
    set_bootstrap_error "Download failed: unable to fetch $ASSET_NAME from $REPO_SLUG releases."
    return 1
  fi

  if ! chmod +x "$binary_path"; then
    set_bootstrap_error "Download failed: unable to make $binary_path executable."
    return 1
  fi

  DOWNLOADED_BINARY=$binary_path
  return 0
}

trap cleanup EXIT INT TERM

parse_args "$@"
if [ "$SHOW_HELP" -eq 1 ]; then
  print_usage
  exit 0
fi

if ! validate_host; then
  fail "$BOOTSTRAP_ERROR"
fi

if download_binary; then
  log "Starting laptop-setup."
  "$DOWNLOADED_BINARY" "$@"
  exit $?
fi

if [ -n "$BOOTSTRAP_ERROR" ]; then
  fail "$BOOTSTRAP_ERROR"
fi

fail "unable to download laptop-setup binary"
