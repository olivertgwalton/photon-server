#!/usr/bin/env bash
# Builds FFmpeg for one target with BtbN/FFmpeg-Builds' gpl variant and writes, into OUT:
# (a library image already loaded under BtbN's name is used instead of being rebuilt)
#   ffmpeg-<tag>-<target>.tar.xz          ffmpeg, ffprobe, COPYING.GPLv3, BUILDCONF
#   ffmpeg-<tag>-source.tar               the Corresponding Source for every target
set -euo pipefail

target="${1:?usage: build.sh linux64|linuxarm64 OUT}"
out="$(mkdir -p "${2:?usage: build.sh linux64|linuxarm64 OUT}" && cd "$2" && pwd)"
here="$(cd "$(dirname "$0")" && pwd)"
source "$here/versions.env"
work="${WORK:-$here/.work}"
addin="${FFMPEG_TAG#n}"
addin="${addin%.*}"

mkdir -p "$work"
if [[ ! -d "$work/btbn/.git" ]]; then
  git clone --filter=blob:none "$BTBN_REPO" "$work/btbn"
fi
git -C "$work/btbn" fetch --quiet origin "$BTBN_COMMIT"
git -C "$work/btbn" checkout --quiet --force "$BTBN_COMMIT"

cd "$work/btbn"
image="$(source util/vars.sh "$target" gpl "$addin" && echo "$IMAGE")"
base="$(source util/vars.sh "$target" gpl "$addin" && echo "$BASE_IMAGE")"
# Either way .cache/downloads ends up holding the library sources the image is built from.
if docker image inspect "$image" >/dev/null 2>&1; then
  docker build -t "$base" images/base
  ./download.sh
else
  NOCLEAN=1 ./makeimage.sh "$target" gpl "$addin"
fi
rm -rf "$work/prefix"
GIT_BRANCH_OVERRIDE="$FFMPEG_TAG" FFBUILD_OUTPUT_DIR="$work/prefix" ./build.sh "$target" gpl "$addin"

pkg="$work/pkg/ffmpeg-$FFMPEG_TAG-$target"
rm -rf "$pkg" && mkdir -p "$pkg"
cp "$work/prefix/bin/ffmpeg" "$work/prefix/bin/ffprobe" "$pkg/"
cp "$work/prefix/LICENSE.txt" "$pkg/COPYING.GPLv3"
case "$target" in
  linux64) platform=linux/amd64 ;;
  linuxarm64) platform=linux/arm64 ;;
esac
docker run --rm --platform "$platform" -v "$pkg:/pkg:ro" debian:trixie-slim \
  /pkg/ffmpeg -hide_banner -buildconf >"$pkg/BUILDCONF"
"$here/verify.sh" "$target" "$pkg/BUILDCONF"
tar -C "$work/pkg" -cJf "$out/ffmpeg-$FFMPEG_TAG-$target.tar.xz" "ffmpeg-$FFMPEG_TAG-$target"

src="$work/source/ffmpeg-$FFMPEG_TAG-source"
rm -rf "$src" && mkdir -p "$src/libraries"
git -C "$work/btbn" archive --prefix=FFmpeg-Builds/ -o "$src/FFmpeg-Builds-$BTBN_COMMIT.tar" HEAD
git clone --quiet --depth 1 --branch "$FFMPEG_TAG" https://github.com/FFmpeg/FFmpeg.git "$work/ffmpeg-src"
git -C "$work/ffmpeg-src" archive --prefix="ffmpeg-$FFMPEG_TAG/" -o "$src/ffmpeg-$FFMPEG_TAG.tar" HEAD
rm -rf "$work/ffmpeg-src"
cp -L .cache/downloads/*.tar.xz "$src/libraries/"
cp "$here/SOURCE.md" "$src/README.md"
tar -C "$work/source" -cf "$out/ffmpeg-$FFMPEG_TAG-source.tar" "ffmpeg-$FFMPEG_TAG-source"
