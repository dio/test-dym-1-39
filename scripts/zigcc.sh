#!/usr/bin/env bash
# scripts/zigcc.sh
# Wraps "zig cc" for use as CC when cross-compiling Go c-shared libraries.
#
# Go's linker passes --unresolved-symbols=ignore-all which LLVM lld (used by
# zig cc) does not support. This wrapper strips that flag before forwarding.
#
# Zig is auto-downloaded into .bin/zig-dist/ if not already present.
# ZIG_VERSION defaults to the version pinned here. Override via ZIG_VERSION env.
# ZIG env var overrides the zig binary path entirely.
# TARGET env var sets the cross-compile triple (default: x86_64-linux-gnu).
#
# Usage (from repo root):
#   TARGET=x86_64-linux-gnu CC=$(pwd)/scripts/zigcc.sh CGO_ENABLED=1 go build ...
set -euo pipefail

SCRIPT_DIR="$(dirname "$(realpath "${BASH_SOURCE[0]}")")"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

ZIG_VERSION="${ZIG_VERSION:-0.16.0}"
TARGET="${TARGET:-x86_64-linux-gnu}"

if [[ -z "${ZIG:-}" ]]; then
    ZIG_BIN="$REPO_ROOT/.bin/zig-dist/zig"

    if [[ ! -x "$ZIG_BIN" ]]; then
        # Detect host for zig download (zig uses aarch64/x86_64 and macos/linux).
        _UNAME_M="$(uname -m)"
        _ARCH="${_UNAME_M/arm64/aarch64}"
        case "$(uname -s)" in
            Darwin) _OS="macos" ;;
            Linux)  _OS="linux" ;;
            *)      echo "zigcc.sh: unsupported OS: $(uname -s)" >&2; exit 1 ;;
        esac
        ZIG_URL="https://ziglang.org/download/${ZIG_VERSION}/zig-${_ARCH}-${_OS}-${ZIG_VERSION}.tar.xz"
        echo "zigcc.sh: downloading zig ${ZIG_VERSION} from ${ZIG_URL} ..." >&2
        mkdir -p "$REPO_ROOT/.bin/zig-dist"
        curl -fsSL "$ZIG_URL" | tar -xJ --strip-components=1 -C "$REPO_ROOT/.bin/zig-dist"
        echo "zigcc.sh: zig ready at $ZIG_BIN" >&2
    fi

    ZIG="$ZIG_BIN"
fi

args=()
for arg in "$@"; do
    [[ "$arg" == "--unresolved-symbols="* ]] && continue
    args+=("$arg")
done

exec "$ZIG" cc -target "$TARGET" "${args[@]}"
