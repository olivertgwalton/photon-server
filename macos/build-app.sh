#!/bin/sh
# build-app.sh VERSION SERVER SERVICES OUT makes OUT/Photon.app for Apple silicon: the menu-bar
# app and the Sparkle framework it updates itself with, with SERVER, a darwin/arm64 release
# archive's photon-server folder, and SERVICES, the postgres and valkey folders build-services.sh
# makes, in its Resources.
set -eu
version=$1 server=$2 services=$3 out=$4
here=$(cd "$(dirname "$0")" && pwd)

swift build -c release --arch arm64 --package-path "$here"
bin=$(swift build -c release --arch arm64 --package-path "$here" --show-bin-path)
app="$out/Photon.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$app/Contents/Frameworks"
cp "$bin/Photon" "$app/Contents/MacOS/Photon"
ditto "$bin/Sparkle.framework" "$app/Contents/Frameworks/Sparkle.framework"
# Sparkle's XPC services are for a sandboxed app: Photon is none, and installs through Autoupdate.
rm -rf "$app/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices"
cp -R "$server" "$app/Contents/Resources/photon-server"
cp -R "$services/postgres" "$services/valkey" "$app/Contents/Resources/"
sed "s/VERSION/${version#v}/g" "$here/Info.plist" > "$app/Contents/Info.plist"

# The icon is the web app's mark, inset as a macOS icon is, at each size macOS asks for.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
sips -s format png -z 824 824 "$here/../web/static/favicon.svg" --out "$work/mark.png" >/dev/null
sips -p 1024 1024 "$work/mark.png" --out "$work/icon.png" >/dev/null
mkdir "$work/AppIcon.iconset"
for size in 16 32 128 256 512; do
  sips -z $size $size "$work/icon.png" --out "$work/AppIcon.iconset/icon_${size}x${size}.png" >/dev/null
  sips -z $((size * 2)) $((size * 2)) "$work/icon.png" --out "$work/AppIcon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns -o "$app/Contents/Resources/AppIcon.icns" "$work/AppIcon.iconset"
# The menu bar's is the mark alone, 18 points square.
sips -s format png -z 18 18 "$here/MenuBarIcon.svg" --out "$app/Contents/Resources/MenuBarIconTemplate.png" >/dev/null
sips -s format png -z 36 36 "$here/MenuBarIcon.svg" --out "$app/Contents/Resources/MenuBarIconTemplate@2x.png" >/dev/null
