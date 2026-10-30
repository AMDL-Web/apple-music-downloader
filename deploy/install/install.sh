#!/bin/sh
# Installs amdl on macOS or Linux from a GitHub release of
# AMDL-Web/apple-music-downloader, verified against the release's SHA256SUMS.
# Running it again upgrades in place. docs/install.md has the details.
#
#   curl -fsSL https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/main/deploy/install/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --service --wrapper http://nas:30020
set -eu

repo=AMDL-Web/apple-music-downloader
version=${AMDL_VERSION:-}
bin_dir=
system=0
service=0
wrapper=
force=0
dry_run=0
# A release directory or URL to install from instead of GitHub; tests point
# it at a local file:// tree.
base_url=${AMDL_INSTALL_BASE_URL:-}

usage() {
	cat <<'USAGE'
usage: install.sh [options]

  --version vX.Y.Z   install this release (default: the latest)
  --bin-dir DIR      install into DIR (default: ~/.local/bin)
  --system           install into /usr/local/bin, with sudo when it is not writable
  --service          run amdl as a background service afterwards (amdl service install)
  --wrapper URL      the wrapper address the service uses on its first start
  --force            reinstall even when this version is already installed
  --dry-run          print what would happen and change nothing
USAGE
}

say() { printf 'amdl-install: %s\n' "$*" >&2; }
die() {
	say "error: $*"
	exit 1
}

while [ $# -gt 0 ]; do
	case $1 in
	--version) [ $# -ge 2 ] || die "--version needs a value"; version=$2; shift ;;
	--bin-dir) [ $# -ge 2 ] || die "--bin-dir needs a value"; bin_dir=$2; shift ;;
	--system) system=1 ;;
	--service) service=1 ;;
	--wrapper) [ $# -ge 2 ] || die "--wrapper needs a value"; wrapper=$2; shift ;;
	--force) force=1 ;;
	--dry-run) dry_run=1 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; die "unknown option: $1" ;;
	esac
	shift
done

# AMDL_INSTALL_OS and AMDL_INSTALL_ARCH override detection, for tests.
case ${AMDL_INSTALL_OS:-$(uname -s)} in
Darwin | darwin) os=darwin ;;
Linux | linux) os=linux ;;
*) die "unsupported system $(uname -s); on Windows use install.ps1" ;;
esac
case ${AMDL_INSTALL_ARCH:-$(uname -m)} in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported CPU $(uname -m); releases are built for amd64 and arm64" ;;
esac
# A shell under Rosetta reports x86_64 on Apple Silicon; install the native build.
if [ -z "${AMDL_INSTALL_ARCH:-}" ] && [ $os = darwin ] && [ $arch = amd64 ] &&
	[ "$(sysctl -in sysctl.proc_translated 2>/dev/null)" = 1 ]; then
	arch=arm64
fi

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 2 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "neither curl nor wget is installed"
	fi
}

ffmpeg_hint() {
	case $os in
	darwin)
		if command -v brew >/dev/null 2>&1; then echo "brew install ffmpeg"
		elif command -v port >/dev/null 2>&1; then echo "sudo port install ffmpeg"
		else echo "install Homebrew from https://brew.sh, then brew install ffmpeg"; fi ;;
	linux)
		if command -v apt-get >/dev/null 2>&1; then echo "sudo apt-get install ffmpeg"
		elif command -v dnf >/dev/null 2>&1; then echo "sudo dnf install ffmpeg-free"
		elif command -v pacman >/dev/null 2>&1; then echo "sudo pacman -S ffmpeg"
		elif command -v apk >/dev/null 2>&1; then echo "sudo apk add ffmpeg"
		else echo "download ffmpeg from https://ffmpeg.org/download.html and put it on PATH"; fi ;;
	esac
}

if [ -z "$bin_dir" ]; then
	if [ $system = 1 ]; then bin_dir=/usr/local/bin; else bin_dir=$HOME/.local/bin; fi
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

if [ -z "$version" ]; then
	[ -z "$base_url" ] || die "--version is needed with AMDL_INSTALL_BASE_URL"
	fetch "https://api.github.com/repos/$repo/releases/latest" "$tmp/latest.json" ||
		die "could not look up the latest release; pass --version"
	version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)
	[ -n "$version" ] || die "the latest release has no tag; pass --version"
fi

name=amdl_${version}_${os}_${arch}
base=${base_url:-https://github.com/$repo/releases/download/$version}
sudo=
target=$bin_dir
while [ ! -d "$target" ]; do target=$(dirname "$target"); done
if [ ! -w "$target" ] && [ "$(id -u)" != 0 ]; then
	[ $system = 1 ] || die "$bin_dir is not writable; pick another --bin-dir or use --system"
	command -v sudo >/dev/null 2>&1 || die "$bin_dir is not writable and sudo is missing; run this as root"
	sudo=sudo
fi

if [ $dry_run = 1 ]; then
	say "would install amdl $version ($os/$arch) from $base/$name.tar.gz"
	say "would verify it against $base/SHA256SUMS"
	say "would write $bin_dir/amdl${sudo:+ with sudo}"
	[ $service = 1 ] && say "would run: amdl service install${wrapper:+ --wrapper $wrapper}"
	exit 0
fi

# An upgrade to the version already there does nothing unless --force. The
# refused AMDL_URL keeps amdl version from looking for a server.
if [ $force = 0 ] && [ -x "$bin_dir/amdl" ] &&
	AMDL_URL=http://127.0.0.1:1 "$bin_dir/amdl" version --fields client 2>/dev/null | grep -q "\"version\":\"$version\""; then
	say "amdl $version is already installed in $bin_dir"
else
	say "downloading amdl $version for $os/$arch"
	fetch "$base/$name.tar.gz" "$tmp/$name.tar.gz" || die "could not download $base/$name.tar.gz"
	fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download $base/SHA256SUMS"
	want=$(awk -v f="$name.tar.gz" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || die "SHA256SUMS lists no $name.tar.gz"
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$tmp/$name.tar.gz" | cut -d ' ' -f 1)
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$tmp/$name.tar.gz" | cut -d ' ' -f 1)
	else
		die "neither sha256sum nor shasum is installed, so the download cannot be checked"
	fi
	[ "$got" = "$want" ] || die "checksum mismatch for $name.tar.gz: got $got, want $want"
	tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
	[ -n "$sudo" ] && say "writing $bin_dir needs root; running sudo"
	# A service the user keeps running is stopped for the swap and started on
	# the new binary; one they stopped stays stopped.
	active=0
	if [ -x "$bin_dir/amdl" ] && "$bin_dir/amdl" service status 2>/dev/null | grep -q '"active":true'; then
		active=1
		say "stopping the amdl service for the upgrade"
		"$bin_dir/amdl" service stop >/dev/null || say "the service did not stop cleanly; replacing the binary anyway"
	fi
	# Written beside the old binary and renamed over it, so nothing sees a
	# half-written one; the old one is kept until the new one has started.
	$sudo mkdir -p "$bin_dir"
	$sudo cp "$tmp/$name/amdl" "$bin_dir/.amdl.new"
	$sudo chmod 0755 "$bin_dir/.amdl.new"
	[ -f "$bin_dir/amdl" ] && $sudo cp -p "$bin_dir/amdl" "$bin_dir/.amdl.prev"
	$sudo mv -f "$bin_dir/.amdl.new" "$bin_dir/amdl"
	if [ $active = 1 ] && ! "$bin_dir/amdl" service start >/dev/null; then
		if [ -f "$bin_dir/.amdl.prev" ]; then
			$sudo mv -f "$bin_dir/.amdl.prev" "$bin_dir/amdl"
			"$bin_dir/amdl" service start >/dev/null || say "the previous binary did not start the service either"
		fi
		die "amdl $version did not start the service; the previous binary is back. Its log: amdl service status"
	fi
	$sudo rm -f "$bin_dir/.amdl.prev"
	say "installed $bin_dir/amdl ($version)"
fi

case ":$PATH:" in
*":$bin_dir:"*) ;;
*) say "$bin_dir is not on PATH; add it: export PATH=\"$bin_dir:\$PATH\"" ;;
esac
if ! command -v ffmpeg >/dev/null 2>&1; then
	say "ffmpeg is missing; amdl needs it to write tracks. Install it with: $(ffmpeg_hint)"
fi

if [ $service = 1 ]; then
	if [ -n "$wrapper" ]; then
		"$bin_dir/amdl" service install --wrapper "$wrapper"
	else
		"$bin_dir/amdl" service install
	fi
fi
