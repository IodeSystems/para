#!/bin/sh
# Install para: curl -fsSL https://para.iodesystems.com/install.sh | sh
#
# Downloads the latest release for this OS and CPU from GitHub, verifies the
# release manifest's minisign signature when minisign is installed, checks the
# tarball's sha256 against the manifest, and puts para in ~/.local/bin
# (PARA_INSTALL_DIR overrides). Runs nothing else.
# Refuses to run as root unless PARA_ALLOW_ROOT=1.
set -eu

dir=${PARA_INSTALL_DIR:-$HOME/.local/bin}
base=https://github.com/IodeSystems/para/releases/latest/download
src=https://github.com/IodeSystems/para

fail() { echo "install.sh: $*" >&2; exit 1; }

if [ "$(id -u)" = 0 ] && [ "${PARA_ALLOW_ROOT:-}" != 1 ]; then
  fail "refusing to run as root; run as your own user, or set PARA_ALLOW_ROOT=1"
fi

case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "no release for $(uname -s); build from source: $src" ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "no release for $(uname -m); build from source: $src" ;;
esac
plat=${os}_${arch}

if command -v curl >/dev/null 2>&1; then
  get() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  get() { wget -qO "$2" "$1"; }
else
  fail "needs curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sum() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "needs sha256sum or shasum"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

get "$base/para-manifest.json" "$tmp/para-manifest.json" || fail "could not download the release manifest"

# Release key, shared with dun (key ID 2CC27A171DE03D42). Same as minisign.pub on this site.
cat > "$tmp/minisign.pub" <<'KEY'
untrusted comment: minisign public key 2CC27A171DE03D42
RWRCPeAdF3rCLFMIlOWIL4mfNDKFhvF8YL3PVH3iOO35Uh6pYdqoCvDR
KEY
if command -v minisign >/dev/null 2>&1; then
  get "$base/para-manifest.json.minisig" "$tmp/para-manifest.json.minisig" || fail "could not download the manifest signature"
  minisign -V -q -p "$tmp/minisign.pub" -m "$tmp/para-manifest.json" -x "$tmp/para-manifest.json.minisig" \
    || fail "the manifest signature does not verify; nothing was installed"
  echo "manifest signature verified (key 2CC27A171DE03D42)"
elif [ "${PARA_UNVERIFIED:-}" = 1 ]; then
  echo "warning: PARA_UNVERIFIED=1 and no minisign: the manifest signature is NOT checked; the sha256 only matches the tarball to an unverified manifest" >&2
else
  fail "minisign is not installed, so the release signature cannot be checked. Install minisign (apt install minisign, brew install minisign) and run this again, or set PARA_UNVERIFIED=1 to install without the check"
fi

# The manifest is written by tools/mkmanifest: one platform per block, its
# sha256 a few lines below the platform's name.
product=$(sed -n 's/^  "product": *"\([^"]*\)".*/\1/p' "$tmp/para-manifest.json" | head -1)
[ "$product" = para ] || fail "this manifest is for '${product:-no product}', not para"
want=$(grep -A3 "\"$plat\": {" "$tmp/para-manifest.json" | sed -n 's/.*"sha256": *"\([0-9a-f]*\)".*/\1/p' | head -1)
version=$(sed -n 's/^  "version": *"\([^"]*\)".*/\1/p' "$tmp/para-manifest.json" | head -1)
[ -n "$want" ] || fail "the manifest lists no $plat build"
# The signature's trusted comment binds the product and version (the key is shared with dun).
if [ -f "$tmp/para-manifest.json.minisig" ]; then
  tc=$(sed -n 's/^trusted comment: //p' "$tmp/para-manifest.json.minisig" | head -1)
  [ "$tc" = "para $version manifest" ] || fail "the signature is for '$tc', not 'para $version manifest'"
fi

echo "para $version for $plat"
get "$base/para_$plat.tar.gz" "$tmp/para.tar.gz" || fail "could not download para_$plat.tar.gz"
got=$(sum "$tmp/para.tar.gz")
[ "$got" = "$want" ] || fail "sha256 mismatch for para_$plat.tar.gz: got $got, the manifest says $want"

tar -xzf "$tmp/para.tar.gz" -C "$tmp" para
mkdir -p "$dir"
install -m 755 "$tmp/para" "$dir/para"
echo "installed $dir/para"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "note: $dir is not on PATH; add it, e.g. export PATH=\"$dir:\$PATH\"" ;;
esac
cat <<NEXT

next:
  para setup    # domains, first vault, service, sign-in, optional agent
NEXT
