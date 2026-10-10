#!/bin/sh
# fetch-jellyfin-web.sh WEB LICENSES puts Jellyfin's web app into WEB and its copyright into
# LICENSES, from Jellyfin's Debian package, pinned by checksum. The Jellyfin API serves it under
# /web/, where Jellyfin's TV apps load it from. The image and the release archives both take it
# from here.
set -eu

VERSION=12.2
SUM=b622ee032cde9db2da1d5945d4b0e11f7a5c38cea0f4a0ac581bc0ac27f3524f

web=$1 licenses=$2
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL --retry 5 -o "$work/web.deb" \
  "https://repo.jellyfin.org/files/server/debian/stable/v$VERSION/amd64/jellyfin-web_$VERSION%2Bdeb13_all.deb"
echo "$SUM  $work/web.deb" | sha256sum -c - >/dev/null
dpkg-deb -x "$work/web.deb" "$work/root"
mkdir -p "$web" "$licenses"
cp -R "$work/root/usr/share/jellyfin/web/." "$web"
cp "$work/root/usr/share/doc/jellyfin-web/copyright" "$licenses/copyright"
