#!/usr/bin/env bash
#
# Sign and notarize the Trinity Installer .app bundle.
#
# Usage:
#   ./scripts/macos-codesign.sh <path-to-app>
#
# Required environment variables:
#   MACOS_SIGNING_IDENTITY   "Developer ID Application: Your Name (TEAMID)"
#   AC_API_KEY_PATH          Path to an App Store Connect API key (.p8)
#   AC_API_KEY_ID            10-char Key ID from App Store Connect
#   AC_API_ISSUER_ID         Issuer UUID from App Store Connect
#
# Assumes the signing identity is importable from the default keychain search list.

set -euo pipefail

if [ $# -ne 1 ]; then
    echo "Usage: $0 <path-to-app>" >&2
    exit 1
fi

APP="$1"

if [ ! -d "$APP" ]; then
    echo "error: app bundle not found at $APP" >&2
    exit 1
fi

: "${MACOS_SIGNING_IDENTITY:?must be set}"
: "${AC_API_KEY_PATH:?must be set}"
: "${AC_API_KEY_ID:?must be set}"
: "${AC_API_ISSUER_ID:?must be set}"

echo ">>> Signing $APP"
echo "    identity: $MACOS_SIGNING_IDENTITY"

# The installer is a plain Go binary: no JIT or library-validation exceptions,
# so hardened runtime needs no entitlements.
EXE="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$APP/Contents/Info.plist")"

# Nested code first, so the bundle's signature covers signed children.
codesign --force --options runtime --timestamp \
    --sign "$MACOS_SIGNING_IDENTITY" \
    "$APP/Contents/MacOS/$EXE"

codesign --force --options runtime --timestamp \
    --sign "$MACOS_SIGNING_IDENTITY" \
    "$APP"

codesign --verify --deep --strict --verbose=2 "$APP"

echo ">>> Submitting to Apple notary service"

NOTARIZE_DIR="$(mktemp -d)"
NOTARIZE_ZIP="$NOTARIZE_DIR/notarize.zip"
trap 'rm -rf "$NOTARIZE_DIR"' EXIT

# notarytool wants a flat archive of the .app; --keepParent keeps the bundle directory inside.
ditto -c -k --sequesterRsrc --keepParent "$APP" "$NOTARIZE_ZIP"

xcrun notarytool submit "$NOTARIZE_ZIP" \
    --key "$AC_API_KEY_PATH" \
    --key-id "$AC_API_KEY_ID" \
    --issuer "$AC_API_ISSUER_ID" \
    --wait

# Stapling lets Gatekeeper accept the app offline.
xcrun stapler staple "$APP"
xcrun stapler validate "$APP"

echo ">>> Signed, notarized, stapled: $APP"
