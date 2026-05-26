#!/bin/sh
# mundane installer — downloads a prebuilt CLI binary from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/paulbellamy/mundane/main/install.sh | sh
#
# Detects OS (linux/darwin) and arch (amd64/arm64) and drops the `mundane`
# binary onto your PATH. Overridable via env:
#   MUNDANE_VERSION      release tag to install (default: latest, e.g. v0.0.1)
#   MUNDANE_INSTALL_DIR  where to install (default: /usr/local/bin, else
#                        ~/.local/bin)
set -eu

REPO="paulbellamy/mundane"
VERSION="${MUNDANE_VERSION:-latest}"

err() {
	echo "mundane: $*" >&2
	exit 1
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) err "unsupported OS: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) err "unsupported architecture: $(uname -m)" ;;
	esac
}

fetch() {
	# fetch <url> <dest>
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2" || err "download failed: $1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1" || err "download failed: $1"
	else
		err "need curl or wget to download"
	fi
}

install_dir() {
	if [ -n "${MUNDANE_INSTALL_DIR:-}" ]; then
		echo "$MUNDANE_INSTALL_DIR"
	elif [ -w /usr/local/bin ] 2>/dev/null; then
		echo /usr/local/bin
	else
		echo "${HOME}/.local/bin"
	fi
}

install_bin() {
	# install_bin <src> <dest>
	destdir="$(dirname "$2")"
	chmod +x "$1"
	if mkdir -p "$destdir" 2>/dev/null && mv "$1" "$2" 2>/dev/null; then
		return
	fi
	if command -v sudo >/dev/null 2>&1; then
		echo "mundane: ${destdir} not writable, retrying with sudo" >&2
		sudo mkdir -p "$destdir" && sudo mv "$1" "$2" && return
	fi
	err "cannot write to ${destdir} (set MUNDANE_INSTALL_DIR to a writable dir)"
}

main() {
	os="$(detect_os)"
	arch="$(detect_arch)"
	asset="mundane_${os}_${arch}.tar.gz"

	if [ "$VERSION" = "latest" ]; then
		url="https://github.com/${REPO}/releases/latest/download/${asset}"
	else
		url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
	fi

	dir="$(install_dir)"
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	echo "mundane: downloading ${url}" >&2
	fetch "$url" "${tmp}/${asset}"
	tar -xzf "${tmp}/${asset}" -C "$tmp" || err "could not extract ${asset}"
	[ -f "${tmp}/mundane" ] || err "tarball did not contain a mundane binary"

	install_bin "${tmp}/mundane" "${dir}/mundane"
	echo "mundane: installed ${VERSION} to ${dir}/mundane" >&2

	case ":${PATH}:" in
	*":${dir}:"*) ;;
	*) echo "mundane: note: ${dir} is not on your PATH — add it to use 'mundane'" >&2 ;;
	esac
}

main
