#!/bin/bash
# Build a signed, notarized, stapled wspr.dmg ready for public download.
#
# One-time setup (only you can do these — they need your Apple account):
#
#   1. Create a "Developer ID Application" certificate:
#        Xcode → Settings → Accounts → [your team] → Manage Certificates
#               → "+" → Developer ID Application
#      (You must be the team's Account Holder to create this kind of cert.)
#
#   2. Store notary credentials under a keychain profile named "wspr-notary":
#        xcrun notarytool store-credentials wspr-notary \
#          --apple-id "you@example.com" \
#          --team-id "YOURTEAMID" \
#          --password "app-specific-password"      # from appleid.apple.com
#
# Then just run:  make dmg     (or: bash release.sh)
set -euo pipefail

APP="wspr.app"
BINARY="wspr"
ENTITLEMENTS="entitlements.plist"
NOTARY_PROFILE="${NOTARY_PROFILE:-wspr-notary}"

VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' Info.plist)"
DMG="wspr-${VERSION}.dmg"

# --- locate the Developer ID Application identity --------------------------
DEV_ID="${DEV_ID:-$(security find-identity -v -p codesigning \
    | grep -o '"Developer ID Application: [^"]*"' | head -1 | tr -d '"')}"

if [ -z "$DEV_ID" ]; then
    echo "ERROR: no 'Developer ID Application' certificate found." >&2
    echo "       Create one in Xcode → Settings → Accounts → Manage Certificates." >&2
    echo "       (An 'Apple Development' cert will NOT work for distribution.)" >&2
    exit 1
fi
echo "==> Signing identity: $DEV_ID"

# --- build the .app bundle ------------------------------------------------
echo "==> Building $BINARY"
go build -o "$BINARY" .

echo "==> Assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp Info.plist "$APP/Contents/Info.plist"
cp "$BINARY" "$APP/Contents/MacOS/$BINARY"
printf 'APPL????' > "$APP/Contents/PkgInfo"

# --- sign with hardened runtime + secure timestamp ------------------------
echo "==> Signing $APP"
codesign --force --options runtime --timestamp \
    --entitlements "$ENTITLEMENTS" \
    --sign "$DEV_ID" "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"

# --- build the DMG --------------------------------------------------------
echo "==> Building $DMG"
rm -f "$DMG"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "wspr" -srcfolder "$STAGE" -ov -format UDZO "$DMG"

# --- notarize + staple ----------------------------------------------------
echo "==> Submitting to Apple notary service (this is the slow part)"
xcrun notarytool submit "$DMG" --keychain-profile "$NOTARY_PROFILE" --wait

echo "==> Stapling notarization ticket"
xcrun stapler staple "$DMG"

echo "==> Verifying Gatekeeper acceptance"
spctl -a -t open --context context:primary-signature -vvv "$DMG" || true

echo ""
echo "Done. Distributable artifact: $DMG"
