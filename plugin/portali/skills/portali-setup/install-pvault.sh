#!/bin/sh
# Installs pvault (macOS, Linux) for the person who runs it, from the release this plugin was
# built with. The expected SHA-256 comes from pvault-checksums.txt, shipped in the plugin and
# not fetched with the download, so a tampered download is refused. No sudo, no PATH or profile
# edit, and nothing from the download runs except the pvault it unpacks, to print its version.
#
#   sh install-pvault.sh
#
# PVAULT_INSTALL_DIR  where to put it (default ~/.local/bin)
# PVAULT_RELEASE_BASE the download base (default the project's GitHub releases); for tests
set -eu

here=$(cd "$(dirname "$0")" && pwd)
base=${PVAULT_RELEASE_BASE:-https://github.com/liammaher/portali/releases/download}
dest_dir=${PVAULT_INSTALL_DIR:-$HOME/.local/bin}

die() { echo "install-pvault: $*" >&2; exit 1; }

[ -f "$here/pvault.version" ] && [ -f "$here/pvault-checksums.txt" ] ||
  die "this copy of the plugin has no release to install from (it was built from source): build pvault with 'make build' instead"
version=$(tr -d ' \r\n' < "$here/pvault.version")
case "$version" in
  ""|*[!0-9A-Za-z.+-]*) die "the version in pvault.version isn't one" ;;
esac

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported system $(uname -s): on Windows use install-pvault.ps1" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) die "unsupported processor $(uname -m)" ;;
esac
asset="pvault_${version}_${os}_${arch}.tar.gz"

if [ -x "$dest_dir/pvault" ] && [ "$("$dest_dir/pvault" version 2>/dev/null || true)" = "pvault $version" ]; then
  echo "pvault $version is already installed"
  echo "PVAULT_PATH=$dest_dir/pvault"
  exit 0
fi

want=$(awk -v a="$asset" '$2 == a { print $1 }' "$here/pvault-checksums.txt")
case "$want" in
  *[!0-9a-f]*|"") die "no checksum for $asset in pvault-checksums.txt" ;;
esac
[ "${#want}" -eq 64 ] || die "the checksum for $asset isn't a SHA-256"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

proto='=https'
[ -z "${PVAULT_RELEASE_BASE:-}" ] || proto='=https,http,file'
echo "downloading $asset"
curl -fsSL --proto "$proto" --tlsv1.2 -o "$tmp/$asset" "$base/v$version/$asset" ||
  die "the download failed ($base/v$version/$asset)"

if command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
elif command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
else
  die "neither shasum nor sha256sum is here to check the download"
fi
[ "$got" = "$want" ] || die "checksum mismatch for $asset: nothing was installed"

# Only the one named file comes out of the archive.
tar -xzf "$tmp/$asset" -C "$tmp" pvault || die "$asset has no pvault in it"
mkdir -p "$dest_dir"
# Remove before copying: a running copy that is overwritten in place is killed on macOS.
rm -f "$dest_dir/pvault"
cp "$tmp/pvault" "$dest_dir/pvault"
chmod 755 "$dest_dir/pvault"

if [ "$os" = darwin ]; then
  # As `make build` does: ad-hoc signed with the hardened runtime, which the vault's own
  # identity checks and the Touch ID helper expect.
  xattr -d com.apple.quarantine "$dest_dir/pvault" 2>/dev/null || true
  codesign -s - -f -o runtime "$dest_dir/pvault" 2>/dev/null ||
    echo "install-pvault: could not sign pvault (codesign); it runs, but Touch ID won't be offered" >&2
fi

[ "$("$dest_dir/pvault" version 2>/dev/null || true)" = "pvault $version" ] ||
  die "the installed pvault doesn't report version $version"
echo "installed pvault $version"
case ":$PATH:" in
  *":$dest_dir:"*) ;;
  *) echo "note: $dest_dir is not on your PATH; run it as $dest_dir/pvault" ;;
esac
echo "PVAULT_PATH=$dest_dir/pvault"
