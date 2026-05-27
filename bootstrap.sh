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
It uses the default macOS shell plus awk, curl, chmod, mktemp, rm, shasum, and uname.
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
    if ! TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t laptop-setup-bootstrap); then
      set_bootstrap_error "Unable to create a temporary directory."
      return 1
    fi
  fi
  return 0
}

asset_digest_from_metadata() {
  metadata_path=$1
  asset_name=$2
  awk -v asset="$asset_name" '
    index($0, "\"name\": \"" asset "\"") { in_asset = 1 }
    in_asset && index($0, "\"digest\": \"sha256:") {
      sub(/^.*"digest": "sha256:/, "")
      sub(/".*$/, "")
      print
      exit
    }
  ' "$metadata_path"
}

verify_binary_checksum() {
  binary_path=$1
  expected_sha256=$2
  if ! actual_sha256=$(shasum -a 256 "$binary_path" | awk '{ print $1 }'); then
    set_bootstrap_error "Checksum verification failed: unable to calculate SHA-256 for $ASSET_NAME."
    return 1
  fi
  if [ "$actual_sha256" != "$expected_sha256" ]; then
    set_bootstrap_error "Checksum verification failed for $ASSET_NAME."
    return 1
  fi
  log "Verified $ASSET_NAME checksum."
  return 0
}

download_binary() {
  if ! require_command awk; then
    return 1
  fi
  if ! require_command curl; then
    return 1
  fi
  if ! require_command chmod; then
    return 1
  fi
  if ! require_command mktemp; then
    return 1
  fi
  if ! require_command shasum; then
    return 1
  fi

  if ! ensure_tmp_dir; then
    return 1
  fi
  binary_path=$TMP_DIR/laptop-setup
  metadata_path=$TMP_DIR/release.json
  url="https://github.com/$REPO_SLUG/releases/latest/download/$ASSET_NAME"
  metadata_url="https://api.github.com/repos/$REPO_SLUG/releases/latest"

  log "Fetching release metadata from $metadata_url."
  if ! curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --retry-delay 2 "$metadata_url" -o "$metadata_path"; then
    set_bootstrap_error "Download failed: unable to fetch latest release metadata from $REPO_SLUG."
    return 1
  fi
  if ! expected_sha256=$(asset_digest_from_metadata "$metadata_path" "$ASSET_NAME"); then
    set_bootstrap_error "Download failed: unable to read latest release metadata from $REPO_SLUG."
    return 1
  fi
  if [ -z "$expected_sha256" ]; then
    set_bootstrap_error "Download failed: unable to find SHA-256 digest for $ASSET_NAME in release metadata."
    return 1
  fi

  log "Downloading laptop-setup binary from $url."
  if ! curl -fL --proto '=https' --tlsv1.2 --retry 3 --retry-delay 2 "$url" -o "$binary_path"; then
    set_bootstrap_error "Download failed: unable to fetch $ASSET_NAME from $REPO_SLUG releases."
    return 1
  fi

  if ! verify_binary_checksum "$binary_path" "$expected_sha256"; then
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
