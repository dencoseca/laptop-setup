#!/usr/bin/env sh

set -eu

REPO_SLUG="dencoseca/laptop-setup"
CHECKSUM_FILE="checksums.txt"
TMP_DIR=""
DOWNLOADED_BINARY=""
ENV_FLAG=""
AUTO_YES=0
SHOW_HELP=0
LEGACY_COMPATIBLE=1

print_usage() {
  cat <<'EOF'
Usage:
  sh bootstrap.sh [flags]

Common flags:
  -e, --environment <home|work>   Environment profile
  -y, --yes                       Non-interactive mode
  -h, --help                      Show usage

All flags are forwarded to the downloaded `laptop-setup` binary.
Set `LAPTOP_SETUP_RELEASE_TAG` to pin to a specific GitHub release tag.
EOF
}

log() {
  printf '%s\n' "$1"
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
  expect_env=0
  for arg in "$@"; do
    if [ "$expect_env" -eq 1 ]; then
      ENV_FLAG="$arg"
      expect_env=0
      continue
    fi

    case "$arg" in
      -e|--environment)
        expect_env=1
        ;;
      --environment=*)
        ENV_FLAG=${arg#*=}
        ;;
      -y|--yes)
        AUTO_YES=1
        ;;
      -h|--help)
        SHOW_HELP=1
        ;;
      *)
        LEGACY_COMPATIBLE=0
        ;;
    esac
  done

  if [ "$expect_env" -eq 1 ]; then
    fail "missing value for --environment/-e"
  fi

  if [ -n "$ENV_FLAG" ] && [ "$ENV_FLAG" != "home" ] && [ "$ENV_FLAG" != "work" ]; then
    fail "invalid environment '$ENV_FLAG' (expected 'home' or 'work')"
  fi
}

resolve_artifact_name() {
  os_name=$(uname -s 2>/dev/null || true)
  if [ "$os_name" != "Darwin" ]; then
    return 1
  fi

  machine_arch=$(uname -m 2>/dev/null || true)
  case "$machine_arch" in
    arm64|aarch64)
      printf 'laptop-setup_darwin_arm64\n'
      ;;
    x86_64|amd64)
      printf 'laptop-setup_darwin_amd64\n'
      ;;
    *)
      return 1
      ;;
  esac
}

release_base_url() {
  release_tag=${LAPTOP_SETUP_RELEASE_TAG:-latest}
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
    log "curl is required to download releases."
    return 1
  fi

  artifact_name=$(resolve_artifact_name) || {
    log "Unsupported host. This bootstrap supports macOS arm64/amd64 releases."
    return 1
  }
  base_url=$(release_base_url)

  TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t laptop-setup-bootstrap)
  checksums_path=$TMP_DIR/$CHECKSUM_FILE
  binary_path=$TMP_DIR/$artifact_name

  log "Downloading release artifact: $artifact_name"
  if ! curl -fsSL "$base_url/$CHECKSUM_FILE" -o "$checksums_path"; then
    log "Failed to download $CHECKSUM_FILE from $base_url."
    return 1
  fi
  if ! curl -fsSL "$base_url/$artifact_name" -o "$binary_path"; then
    log "Failed to download $artifact_name from $base_url."
    return 1
  fi

  expected_sum=$(awk -v artifact="$artifact_name" '$2 == artifact || $2 == ("*" artifact) {print $1; exit}' "$checksums_path")
  if [ -z "$expected_sum" ]; then
    log "Checksum entry for $artifact_name not found in $CHECKSUM_FILE."
    return 1
  fi

  actual_sum=$(compute_sha256 "$binary_path") || {
    log "No SHA256 tool found (expected shasum or sha256sum)."
    return 1
  }

  if [ "$expected_sum" != "$actual_sum" ]; then
    log "Checksum verification failed for $artifact_name."
    return 1
  fi

  chmod +x "$binary_path"
  DOWNLOADED_BINARY=$binary_path
  return 0
}

ensure_xcode_clt() {
  if xcode-select -p >/dev/null 2>&1; then
    return 0
  fi

  log "Command Line Tools for Xcode not found. Attempting installation."
  touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
  version=$(softwareupdate -l 2>/dev/null | grep "\*.*Command Line" | tail -n 1 | sed 's/^[^C]* //')
  if [ -z "$version" ]; then
    log "Unable to find an installable Command Line Tools package."
    return 1
  fi

  if ! softwareupdate -i "$version" --verbose >> "$HOME/.xcode-select-install.log" 2>&1; then
    log "Command Line Tools installation failed. See $HOME/.xcode-select-install.log."
    return 1
  fi
}

run_legacy_fallback() {
  if [ "$LEGACY_COMPATIBLE" -ne 1 ]; then
    log "Legacy fallback skipped: arguments are not compatible with setup.sh."
    return 1
  fi
  if [ -z "$ENV_FLAG" ]; then
    log "Legacy fallback skipped: --environment/-e is required for setup.sh."
    return 1
  fi

  if ! ensure_xcode_clt; then
    log "Legacy fallback failed while preparing prerequisites."
    return 1
  fi
  if ! command -v git >/dev/null 2>&1; then
    log "Legacy fallback requires git."
    return 1
  fi

  target_dir=$HOME/Developer/repos/github.com/dencoseca/laptop-setup
  mkdir -p "$HOME/Developer/repos/github.com/dencoseca"
  if [ ! -d "$target_dir/.git" ]; then
    log "Cloning repository for legacy setup fallback."
    if ! git clone "https://github.com/$REPO_SLUG.git" "$target_dir"; then
      log "Failed to clone repository for legacy fallback."
      return 1
    fi
  fi

  chmod +x "$target_dir/setup.sh"
  if [ "$AUTO_YES" -eq 1 ] || [ ! -t 0 ]; then
    "$target_dir/setup.sh" -e "$ENV_FLAG" -y
  else
    "$target_dir/setup.sh" -e "$ENV_FLAG"
  fi
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

log "Binary bootstrap failed. Attempting legacy setup.sh fallback."
if run_legacy_fallback; then
  exit 0
fi

fail "unable to start laptop-setup binary or legacy fallback"
