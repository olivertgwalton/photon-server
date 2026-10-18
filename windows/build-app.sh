#!/bin/sh
# build-app.sh VERSION SERVER SERVICES OUT makes OUT/photon-server_VERSION_windows_amd64_setup.exe,
# the installer of Photon for Windows: the tray app, with SERVER, a windows/amd64 release
# archive's photon-server folder, and SERVICES, the postgres and valkey folders build-services.sh
# makes, beside it. It needs go, makensis, rsvg-convert and magick on the PATH.
set -eu
version=$1 server=$2 services=$3 out=$4
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

app="$work/app"
mkdir -p "$app"
(cd "$here/.." && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$version" \
  -o "$app/Photon.exe" ./cmd/photon-tray)
cp -R "$server" "$app/photon-server"
cp -R "$services/postgres" "$services/valkey" "$app/"

# The icon is the web app's mark, inset as a Windows icon is, at each size Windows asks for.
rsvg-convert -w 216 -h 216 "$here/../web/static/favicon.svg" -o "$work/mark.png"
magick "$work/mark.png" -background none -gravity center -extent 256x256 \
  -define icon:auto-resize=256,64,48,32,24,16 "$app/Photon.ico"

mkdir -p "$out"
makensis -V2 -DVERSION="${version#v}" -DAPP="$(cygpath -w "$app")" \
  -DOUT="$(cygpath -wa "$out")\\photon-server_${version}_windows_amd64_setup.exe" "$(cygpath -w "$here/photon.nsi")"
