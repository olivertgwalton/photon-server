#!/usr/bin/env bash
# Fails unless BUILDCONF is a redistributable GPLv3 build with every library the server's
# pipelines name.
set -euo pipefail

target="${1:?usage: verify.sh linux64|linuxarm64 BUILDCONF}"
conf="$(<"${2:?usage: verify.sh linux64|linuxarm64 BUILDCONF}")"

required=(
  --enable-gpl --enable-version3
  --enable-libx264 --enable-libx265 --enable-libsvtav1 --enable-libdav1d --enable-libvpx
  --enable-libopus --enable-libmp3lame
  --enable-libass --enable-libfreetype --enable-libharfbuzz --enable-libfribidi
  --enable-libzimg --enable-libxml2
  --enable-vulkan --enable-libplacebo --enable-opencl --enable-vaapi
  --enable-ffnvcodec --enable-cuda-llvm
  --enable-chromaprint
)
[[ "$target" == linux64 ]] && required+=(--enable-libvpl)
forbidden=(--enable-nonfree --enable-libfdk-aac --enable-cuda-nvcc --enable-libnpp)

status=0
for flag in "${required[@]}"; do
  grep -qxF -- "$flag" <<<"${conf//[[:space:]]/$'\n'}" || { echo "missing $flag" >&2; status=1; }
done
for flag in "${forbidden[@]}"; do
  grep -qxF -- "$flag" <<<"${conf//[[:space:]]/$'\n'}" && { echo "forbidden $flag" >&2; status=1; }
done
exit "$status"
