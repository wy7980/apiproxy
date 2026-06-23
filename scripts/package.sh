#!/usr/bin/env bash
#
# package.sh — build apiproxy and bundle it for distribution.
#
# Usage:
#   ./scripts/package.sh                          # current platform, version from git
#   ./scripts/package.sh -v 1.0.0                 # explicit version tag
#   ./scripts/package.sh -o /tmp/dist              # custom output directory
#   ./scripts/package.sh --os linux --arch amd64   # cross-compile target
#   ./scripts/package.sh --skip-test               # skip go test
#   ./scripts/package.sh --all-platforms           # build linux/amd64 + linux/arm64 + darwin/amd64 + darwin/arm64
#
# Output layout (per platform):
#   apiproxy-{version}-{os}-{arch}.tar.gz
#     ├── apiproxy              # binary
#     ├── configs/
#     │   └── apiproxy.yaml     # example config
#     └── README.md             # project readme
#
set -euo pipefail

# ── defaults ────────────────────────────────────────────────────────────────
VERSION=""
OUTDIR="dist"
GOOS=""
GOARCH=""
SKIP_TEST=false
ALL_PLATFORMS=false
BINARY_NAME="apiproxy"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# ── parse args ──────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    -v|--version)   VERSION="$2"; shift 2 ;;
    -o|--output)    OUTDIR="$2"; shift 2 ;;
    --os)           GOOS="$2"; shift 2 ;;
    --arch)         GOARCH="$2"; shift 2 ;;
    --skip-test)    SKIP_TEST=true; shift ;;
    --all-platforms) ALL_PLATFORMS=true; shift ;;
    -h|--help)
      echo "Usage: $0 [-v VERSION] [-o OUTDIR] [--os GOOS] [--arch GOARCH] [--skip-test] [--all-platforms]"
      echo ""
      echo "  -v, --version     Version string (default: git tag or short commit hash)"
      echo "  -o, --output      Output directory (default: dist)"
      echo "  --os              Target OS for cross-compile (default: current OS)"
      echo "  --arch            Target arch for cross-compile (default: current arch)"
      echo "  --skip-test       Skip go test before build"
      echo "  --all-platforms   Build all 4 common platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)"
      echo "  -h, --help        Show this help"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── resolve version ─────────────────────────────────────────────────────────
if [[ -z "$VERSION" ]]; then
  # Prefer git tag (e.g. v1.2.3 → 1.2.3), else short commit hash.
  TAG="$(git -C "$REPO_ROOT" describe --tags --exact-match 2>/dev/null || true)"
  if [[ -n "$TAG" ]]; then
    VERSION="${TAG#v}"
  else
    VERSION="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
  fi
fi

# ── resolve target platform ────────────────────────────────────────────────
if [[ -z "$GOOS" ]];   then GOOS="$(go env GOOS)";   fi
if [[ -z "$GOARCH" ]]; then GOARCH="$(go env GOARCH)"; fi

PLATFORMS=()
if [[ "$ALL_PLATFORMS" == true ]]; then
  PLATFORMS=("linux:amd64" "linux:arm64" "darwin:amd64" "darwin:arm64")
else
  PLATFORMS=("$GOOS:$GOARCH")
fi

# ── helper: build & package one platform ────────────────────────────────────
build_and_package() {
  local target_os="$1"
  local target_arch="$2"
  local stamp_dir="$OUTDIR/apiproxy-${VERSION}-${target_os}-${target_arch}"
  local tarball="$OUTDIR/apiproxy-${VERSION}-${target_os}-${target_arch}.tar.gz"

  echo "─── Building for ${target_os}/${target_arch} ───"

  # Compile.
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o "$stamp_dir/$BINARY_NAME" \
    "$REPO_ROOT/cmd/apiproxy"

  # Bundle runtime files.
  mkdir -p "$stamp_dir/configs"
  cp "$REPO_ROOT/configs/apiproxy.yaml" "$stamp_dir/configs/apiproxy.yaml"
  cp "$REPO_ROOT/README.md" "$stamp_dir/README.md"

  # Create tarball (relative paths inside the archive so it extracts cleanly).
  tar -czf "$tarball" -C "$OUTDIR" \
    "apiproxy-${VERSION}-${target_os}-${target_arch}"

  # Clean up staging dir.
  rm -rf "$stamp_dir"

  local size
  size="$(du -h "$tarball" 2>/dev/null | cut -f1 || stat --printf='%s' "$tarball" 2>/dev/null || wc -c < "$tarball")"
  echo "    ✔ $tarball  (${size})"
}

# ── run tests unless skipped ────────────────────────────────────────────────
if [[ "$SKIP_TEST" != true ]]; then
  echo "─── Running tests ───"
  go test "$REPO_ROOT/..." -count=1
  echo "    ✔ All tests passed"
fi

# ── build each platform ────────────────────────────────────────────────────
mkdir -p "$OUTDIR"

for plat in "${PLATFORMS[@]}"; do
  os="${plat%%:*}"
  arch="${plat##*:}"
  build_and_package "$os" "$arch"
done

echo ""
echo "Done. Artifacts in $OUTDIR/:"
ls -lh "$OUTDIR"/apiproxy-*.tar.gz 2>/dev/null || echo "  (no tarballs found)"
