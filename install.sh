#!/usr/bin/env bash
set -euo pipefail

PROJECT_NAME="panemux"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"
REPO="${PANEMUX_REPO:-}"
VERSION="${PANEMUX_VERSION:-latest}"
INSTALL_DIR="${PANEMUX_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

usage() {
  cat <<'EOF'
Install PaneMux from GitHub Releases.

Usage:
  install.sh [--repo owner/name] [--version v1.2.3|latest] [--install-dir /path]

Environment variables:
  PANEMUX_REPO         GitHub repository in owner/name form
  PANEMUX_VERSION      Release tag or "latest"
  PANEMUX_INSTALL_DIR  Directory to install the binary into

Examples:
  ./install.sh --repo owner/panemux
  curl -fsSL https://raw.githubusercontent.com/owner/panemux/main/install.sh | bash -s -- --repo owner/panemux
  PANEMUX_REPO=owner/panemux PANEMUX_VERSION=v1.0.0 ./install.sh
EOF
}

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo)
        [[ $# -ge 2 ]] || die "--repo requires a value"
        REPO="$2"
        shift 2
        ;;
      --version)
        [[ $# -ge 2 ]] || die "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --install-dir)
        [[ $# -ge 2 ]] || die "--install-dir requires a value"
        INSTALL_DIR="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done
}

detect_os() {
  local uname_os
  uname_os="$(uname -s)"
  case "$uname_os" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) die "unsupported OS: $uname_os" ;;
  esac
}

detect_arch() {
  local uname_arch
  uname_arch="$(uname -m)"
  case "$uname_arch" in
    x86_64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) die "unsupported architecture: $uname_arch" ;;
  esac
}

fetch_release_json() {
  local url
  if [[ "$VERSION" == "latest" ]]; then
    url="https://api.github.com/repos/${REPO}/releases/latest"
  else
    url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
  fi
  curl -fsSL "$url"
}

parse_release_value() {
  local key="$1"
  python3 - "$key" <<'PY'
import json
import sys

key = sys.argv[1]
data = json.load(sys.stdin)
value = data.get(key)
if value is None:
    sys.exit(1)
print(value)
PY
}

find_asset_url() {
  local asset_name="$1"
  python3 - "$asset_name" <<'PY'
import json
import sys

target = sys.argv[1]
data = json.load(sys.stdin)
for asset in data.get("assets", []):
    if asset.get("name") == target:
        print(asset["browser_download_url"])
        raise SystemExit(0)
raise SystemExit(1)
PY
}

verify_checksum() {
  local archive_name="$1"
  local archive_path="$2"
  local checksums_path="$3"
  if command -v shasum >/dev/null 2>&1; then
    (
      cd "$(dirname "$checksums_path")"
      shasum -a 256 -c <(grep " ${archive_name}\$" "$checksums_path")
    )
  elif command -v sha256sum >/dev/null 2>&1; then
    (
      cd "$(dirname "$checksums_path")"
      sha256sum -c <(grep " ${archive_name}\$" "$checksums_path")
    )
  else
    log "Skipping checksum verification: no shasum or sha256sum available"
  fi
}

main() {
  parse_args "$@"
  need_cmd curl
  need_cmd tar
  need_cmd python3

  [[ -n "$REPO" ]] || die "GitHub repo is required. Pass --repo owner/name or set PANEMUX_REPO."

  local os arch release_json tag archive_name archive_url checksums_url
  os="$(detect_os)"
  arch="$(detect_arch)"

  release_json="$(fetch_release_json)"
  tag="$(printf '%s' "$release_json" | parse_release_value tag_name)" || die "failed to resolve release tag"

  archive_name="${PROJECT_NAME}_${tag}_${os}_${arch}.tar.gz"
  checksums_name="checksums.txt"
  archive_url="$(printf '%s' "$release_json" | find_asset_url "$archive_name")" || die "release asset not found: $archive_name"
  checksums_url="$(printf '%s' "$release_json" | find_asset_url "$checksums_name")" || die "checksums.txt not found in release"

  local tmpdir archive_path checksums_path
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT
  archive_path="${tmpdir}/${archive_name}"
  checksums_path="${tmpdir}/${checksums_name}"

  log "Downloading ${archive_name}"
  curl -fsSL "$archive_url" -o "$archive_path"
  curl -fsSL "$checksums_url" -o "$checksums_path"
  verify_checksum "$archive_name" "$archive_path" "$checksums_path"

  mkdir -p "$INSTALL_DIR"
  tar -xzf "$archive_path" -C "$tmpdir"
  install -m 0755 "${tmpdir}/${PROJECT_NAME}" "${INSTALL_DIR}/${PROJECT_NAME}"

  log "Installed ${PROJECT_NAME} ${tag} to ${INSTALL_DIR}/${PROJECT_NAME}"
  case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      log "Add ${INSTALL_DIR} to PATH if it is not already present."
      ;;
  esac
  log "Try: ${PROJECT_NAME} --help"
}

main "$@"
