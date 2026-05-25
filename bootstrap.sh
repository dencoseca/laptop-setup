#!/usr/bin/env sh

set -eu

REPO_SLUG="dencoseca/laptop-setup"
DEFAULT_RELEASE_TAG="v0.1.0"
CHECKSUM_FILE="checksums.txt"
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
  printf 'Default release tag: %s\n' "$DEFAULT_RELEASE_TAG"
  cat <<'EOF'
Set `LAPTOP_SETUP_RELEASE_TAG` to override (for example: v0.1.1 or latest).
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

resolve_artifact_name() {
  os_name=$1
  machine_arch=$2

  if [ "$os_name" != "Darwin" ]; then
    return 1
  fi

  case "$machine_arch" in
    arm64|aarch64)
      printf 'laptop-setup_darwin_arm64\n'
      ;;
    *)
      return 1
      ;;
  esac
}

release_base_url() {
  release_tag=${LAPTOP_SETUP_RELEASE_TAG:-$DEFAULT_RELEASE_TAG}
  if [ "$release_tag" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download\n' "$REPO_SLUG"
  else
    printf 'https://github.com/%s/releases/download/%s\n' "$REPO_SLUG" "$release_tag"
  fi
}

compute_sha256() {
  file_path=$1
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
    return 0
  fi
  return 1
}

download_and_verify_binary() {
  if ! command -v curl >/dev/null 2>&1; then
    set_bootstrap_error "Missing prerequisite: curl is required to download release artifacts."
    return 1
  fi

  os_name=$(uname -s 2>/dev/null || printf 'unknown')
  machine_arch=$(uname -m 2>/dev/null || printf 'unknown')
  if ! artifact_name=$(resolve_artifact_name "$os_name" "$machine_arch"); then
    set_bootstrap_error "Unsupported host: os=$os_name arch=$machine_arch. Supported target: Apple Silicon macOS (Darwin arm64/aarch64)."
    return 1
  fi
  base_url=$(release_base_url)

  TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t laptop-setup-bootstrap)
  checksums_path=$TMP_DIR/$CHECKSUM_FILE
  binary_path=$TMP_DIR/$artifact_name

  log "Downloading release artifact: $artifact_name"
  if ! curl -fsSL "$base_url/$CHECKSUM_FILE" -o "$checksums_path"; then
    set_bootstrap_error "Download failed: unable to fetch $CHECKSUM_FILE from $base_url/$CHECKSUM_FILE."
    return 1
  fi
  if ! curl -fsSL "$base_url/$artifact_name" -o "$binary_path"; then
    set_bootstrap_error "Download failed: unable to fetch $artifact_name from $base_url/$artifact_name."
    return 1
  fi

  expected_sum=$(awk -v artifact="$artifact_name" '$2 == artifact || $2 == ("*" artifact) {print $1; exit}' "$checksums_path")
  if [ -z "$expected_sum" ]; then
    set_bootstrap_error "Checksum lookup failed: $artifact_name is missing from $CHECKSUM_FILE."
    return 1
  fi

  if ! actual_sum=$(compute_sha256 "$binary_path"); then
    set_bootstrap_error "Missing checksum tool: install either shasum or sha256sum."
    return 1
  fi

  if [ "$expected_sum" != "$actual_sum" ]; then
    set_bootstrap_error "Checksum mismatch for $artifact_name (expected $expected_sum, got $actual_sum)."
    return 1
  fi

  chmod +x "$binary_path"
  DOWNLOADED_BINARY=$binary_path
  return 0
}

trap cleanup EXIT INT TERM

parse_args "$@"
if [ "$SHOW_HELP" -eq 1 ]; then
  print_usage
  exit 0
fi

if download_and_verify_binary; then
  log "Checksum verified. Starting laptop-setup."
  "$DOWNLOADED_BINARY" "$@"
  exit $?
fi

if [ -n "$BOOTSTRAP_ERROR" ]; then
  fail "$BOOTSTRAP_ERROR"
fi

fail "unable to download or verify laptop-setup binary"
