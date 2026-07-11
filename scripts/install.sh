#!/bin/sh
set -eu

repository="${MUHIYA_REPOSITORY:-muhiya/muhiyacode}"
version="${MUHIYA_VERSION:-latest}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$version" = latest ]; then
  version=$(curl -fsSL -H "User-Agent: MuhiyaCode-Installer" "https://api.github.com/repos/$repository/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
fi
[ -n "$version" ] || { echo "Could not resolve a release version." >&2; exit 1; }
clean_version=${version#v}
asset="muhiyacode_${clean_version}_${os}_${arch}.tar.gz"
base="https://github.com/$repository/releases/download/$version"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/muhiyacode-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

curl -fsSL -H "User-Agent: MuhiyaCode-Installer" "$base/$asset" -o "$tmp/$asset"
curl -fsSL -H "User-Agent: MuhiyaCode-Installer" "$base/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v file="$asset" '$2 == file { print $1; exit }' "$tmp/checksums.txt")
[ -n "$expected" ] || { echo "Checksum entry not found for $asset" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || { echo "Checksum mismatch for $asset" >&2; exit 1; }
tar -xzf "$tmp/$asset" -C "$tmp"

install_dir=${MUHIYA_INSTALL_DIR:-}
if [ -z "$install_dir" ]; then
  if [ -w /usr/local/bin ]; then install_dir=/usr/local/bin; else install_dir="$HOME/.local/bin"; fi
fi
mkdir -p "$install_dir"
install -m 0755 "$tmp/muhiyacode" "$install_dir/muhiyacode"
ln -sf "$install_dir/muhiyacode" "$install_dir/muhiya"
echo "Installed MuhiyaCode $version to $install_dir/muhiyacode"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to PATH, then run: muhiyacode doctor --offline" ;;
esac
