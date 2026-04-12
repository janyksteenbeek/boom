#!/usr/bin/env bash
# Bundle libportaudio.dylib into a packaged Boom.app so end users don't
# need `brew install portaudio`. The script:
#
#   1. Locates the libportaudio reference in the binary via otool.
#   2. Resolves any symlinks to the real .dylib file.
#   3. Copies it into Boom.app/Contents/Frameworks/.
#   4. Rewrites the binary's load command (and the dylib's own LC_ID)
#      to point at @executable_path/../Frameworks/<name>.
#   5. Re-signs the .app with an ad-hoc signature so install_name_tool's
#      mutation doesn't break the existing signature on Apple Silicon.
#
# Usage: scripts/bundle-portaudio-darwin.sh <path/to/Boom.app>

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: $0 <path/to/Boom.app>" >&2
    exit 1
fi

APP="$1"
if [[ ! -d "$APP" ]]; then
    echo "error: $APP is not a directory" >&2
    exit 1
fi

BIN="$APP/Contents/MacOS/boom"
if [[ ! -x "$BIN" ]]; then
    echo "error: $BIN missing or not executable" >&2
    exit 1
fi

FRAMEWORKS="$APP/Contents/Frameworks"
mkdir -p "$FRAMEWORKS"

# Find the libportaudio reference the binary actually links to.
SRC_REF=$(otool -L "$BIN" | awk '/libportaudio/ {print $1; exit}')
if [[ -z "$SRC_REF" ]]; then
    echo "error: $BIN does not link to libportaudio (nothing to bundle)" >&2
    exit 1
fi

# Resolve symlinks to the actual file we want to copy.
if [[ -L "$SRC_REF" ]]; then
    REAL=$(readlink "$SRC_REF")
    case "$REAL" in
        /*) SRC_FILE="$REAL" ;;
        *)  SRC_FILE="$(dirname "$SRC_REF")/$REAL" ;;
    esac
else
    SRC_FILE="$SRC_REF"
fi

LIB_NAME=$(basename "$SRC_FILE")
DST="$FRAMEWORKS/$LIB_NAME"

echo "  bundling $SRC_FILE → $DST"
cp -f "$SRC_FILE" "$DST"
chmod u+w "$DST"

# Patch the binary so it loads the bundled copy at runtime.
NEW_REF="@executable_path/../Frameworks/$LIB_NAME"
install_name_tool -change "$SRC_REF" "$NEW_REF" "$BIN"
install_name_tool -id      "$NEW_REF" "$DST"

# Ad-hoc re-sign — install_name_tool invalidates any existing signature
# on Apple Silicon and the .app then refuses to launch.
codesign --force --deep --sign - "$APP"

echo "  done. otool -L now reports:"
otool -L "$BIN" | awk '/libportaudio/ {print "    " $0}'
