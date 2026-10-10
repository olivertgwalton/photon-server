#!/bin/sh
# fetch-tools.sh GOOS GOARCH DIR puts the media tools the server runs into DIR: jellyfin-ffmpeg's
# portable ffmpeg and ffprobe, and yt-dlp's standalone build, for themes from ThemerrDB's links,
# each pinned by checksum. The image and the release archives both take theirs from here.
set -eu

FFMPEG_VERSION=8.1.3-1
YTDLP_VERSION=2026.08.19

goos=$1 goarch=$2 dir=$3
case "$goos/$goarch" in
  linux/amd64)
    ffmpeg=linux64-gpl.tar.xz ffmpeg_sum=b86dc023c64a5a9a410e6e3a938a970460214fae78f31c1553c9f88f1c289b6a
    ytdlp=yt-dlp_linux ytdlp_sum=58162f9bfdc27458ea47bfcb311cf47028f17d8154a8bf7d689861d46399230a ;;
  linux/arm64)
    ffmpeg=linuxarm64-gpl.tar.xz ffmpeg_sum=7bc8e8d0986f7f4693f63e9728570f671f07b6e21c05d5465dd7ce461d1631dc
    ytdlp=yt-dlp_linux_aarch64 ytdlp_sum=b16e4dab368a816cd05d477d698a605a6ae87ccee1c8ffd38fa21d7254141fcc ;;
  darwin/amd64)
    ffmpeg=mac64-gpl.tar.xz ffmpeg_sum=cb2b5c154d49a6b6bfe29fa6c16510e85dcbf60b805764121a167b093d4bb6fb
    ytdlp=yt-dlp_macos ytdlp_sum=0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202 ;;
  darwin/arm64)
    ffmpeg=macarm64-gpl.tar.xz ffmpeg_sum=22445d7299742749ad2eeb9ce87963d50def0357e45b3e6c7b69987c8365dbf6
    ytdlp=yt-dlp_macos ytdlp_sum=0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202 ;;
  windows/amd64)
    ffmpeg=win64-clang-gpl.zip ffmpeg_sum=3691f8b2d4c39c511b5faff1c8c4687bd782dac823e63ba69f0755f7ce1af9ce
    ytdlp=yt-dlp.exe ytdlp_sum=66674953fe251b89f4d08c5f0e35e0728679bd67ab3d7d05c0562af101dd3e7a ;;
  *) echo "no media tools for $goos/$goarch" >&2; exit 2 ;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fetch() { # URL SHA256 FILE
  curl -fsSL --retry 5 -o "$3" "$1"
  echo "$2  $3" | sha256sum -c - >/dev/null
}

mkdir -p "$dir"
fetch "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v$FFMPEG_VERSION/jellyfin-ffmpeg_${FFMPEG_VERSION}_portable_$ffmpeg" \
  "$ffmpeg_sum" "$work/$ffmpeg"
case "$ffmpeg" in
  *.zip) unzip -q -o "$work/$ffmpeg" ffmpeg.exe ffprobe.exe -d "$dir" ;;
  *) tar -xJf "$work/$ffmpeg" -C "$dir" ffmpeg ffprobe ;;
esac
exe=
[ "$goos" = windows ] && exe=.exe
fetch "https://github.com/yt-dlp/yt-dlp/releases/download/$YTDLP_VERSION/$ytdlp" "$ytdlp_sum" "$dir/yt-dlp$exe"
chmod 755 "$dir/yt-dlp$exe"
