#!/bin/sh
# build-app.sh VERSION SERVER SERVICES OUT makes OUT/Photon.app for Apple silicon: the menu-bar
# app, with SERVER, a darwin/arm64 release archive's photon-server folder, and SERVICES, the
# postgres and valkey folders build-services.sh makes, in its Resources.
set -eu
version=$1 server=$2 services=$3 out=$4
here=$(cd "$(dirname "$0")" && pwd)

swift build -c release --arch arm64 --package-path "$here"
app="$out/Photon.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$(swift build -c release --arch arm64 --package-path "$here" --show-bin-path)/Photon" "$app/Contents/MacOS/Photon"
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
