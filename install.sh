#!/bin/sh
#
# Install AnchorDB from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/jolovicdev/anchor-db/master/install.sh | sh
#
# Environment:
#   ANCHORDB_VERSION      version to install, e.g. v1.0.1 (default: latest)
#   ANCHORDB_INSTALL_DIR  where to put the binaries (default: ~/.local/bin)

set -eu

REPO="jolovicdev/anchor-db"
INSTALL_DIR="${ANCHORDB_INSTALL_DIR:-${HOME}/.local/bin}"

die() {
	echo "install: $*" >&2
	exit 1
}

need() {
	command -v "$1" > /dev/null 2>&1 || die "$1 is required but was not found"
}

need uname
need mkdir
need tar

# One of these is enough to download with.
if command -v curl > /dev/null 2>&1; then
	fetch() { curl -fsSL "$1"; }
	fetch_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget > /dev/null 2>&1; then
	fetch() { wget -qO- "$1"; }
	fetch_to() { wget -qO "$2" "$1"; }
else
	die "either curl or wget is required"
fi

os="$(uname -s)"
case "${os}" in
	Linux) goos=linux ;;
	Darwin) goos=darwin ;;
	*) die "unsupported operating system: ${os} (Windows users: download the .zip from the releases page)" ;;
esac

arch="$(uname -m)"
case "${arch}" in
	x86_64 | amd64) goarch=amd64 ;;
	arm64 | aarch64) goarch=arm64 ;;
	*) die "unsupported architecture: ${arch}" ;;
esac

tag="${ANCHORDB_VERSION:-}"
if [ -z "${tag}" ]; then
	# The redirect target of /releases/latest names the tag, which avoids
	# depending on the API (rate limited) or on a JSON parser being present.
	if command -v curl > /dev/null 2>&1; then
		location="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
	else
		location="$(wget --max-redirect=10 -S --spider "https://github.com/${REPO}/releases/latest" 2>&1 | awk '/^ *Location:/ { print $2 }' | tail -n 1)"
	fi
	tag="${location##*/}"
fi
[ -n "${tag}" ] || die "could not determine the latest version; set ANCHORDB_VERSION"

version="${tag#v}"
name="anchordb_${version}_${goos}_${goarch}"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT INT TERM

echo "install: downloading AnchorDB ${tag} for ${goos}/${goarch}"
fetch_to "${base}/${name}.tar.gz" "${tmp}/${name}.tar.gz" \
	|| die "no release archive for ${goos}/${goarch} in ${tag}"

# Verify against the release checksums when a hashing tool is available. A
# missing tool is not fatal, but a mismatch is.
if fetch "${base}/checksums.txt" > "${tmp}/checksums.txt" 2> /dev/null; then
	expected="$(awk -v f="${name}.tar.gz" '$2 == f || $2 == "*" f { print $1 }' "${tmp}/checksums.txt")"
	if [ -n "${expected}" ]; then
		if command -v sha256sum > /dev/null 2>&1; then
			actual="$(sha256sum "${tmp}/${name}.tar.gz" | awk '{ print $1 }')"
		elif command -v shasum > /dev/null 2>&1; then
			actual="$(shasum -a 256 "${tmp}/${name}.tar.gz" | awk '{ print $1 }')"
		else
			actual=""
			echo "install: no sha256 tool found, skipping checksum verification" >&2
		fi
		if [ -n "${actual}" ] && [ "${actual}" != "${expected}" ]; then
			die "checksum mismatch for ${name}.tar.gz (expected ${expected}, got ${actual})"
		fi
	fi
fi

tar xzf "${tmp}/${name}.tar.gz" -C "${tmp}"

mkdir -p "${INSTALL_DIR}"
for binary in anchordb-mcp anchorctl anchord; do
	[ -f "${tmp}/${name}/${binary}" ] || die "${binary} missing from the archive"
	# Replacing a running binary in place fails on some systems; moving the old
	# one aside first does not.
	rm -f "${INSTALL_DIR}/${binary}.old"
	[ -e "${INSTALL_DIR}/${binary}" ] && mv "${INSTALL_DIR}/${binary}" "${INSTALL_DIR}/${binary}.old"
	mv "${tmp}/${name}/${binary}" "${INSTALL_DIR}/${binary}"
	chmod +x "${INSTALL_DIR}/${binary}"
	rm -f "${INSTALL_DIR}/${binary}.old"
done

echo "install: installed anchordb-mcp, anchorctl, and anchord into ${INSTALL_DIR}"

case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*)
		echo "install: ${INSTALL_DIR} is not on your PATH. Add it with:"
		echo "install:   export PATH=\"\$PATH:${INSTALL_DIR}\""
		;;
esac

command -v git > /dev/null 2>&1 || echo "install: git was not found, and AnchorDB needs it at runtime" >&2
