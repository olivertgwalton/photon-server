#!/usr/bin/env bash
# Runs inside the image and fails unless its ffmpeg can do what the server asks of it: the
# hardware paths jellyfin-ffmpeg is built with, its tone mappers, the encoders, and subtitles
# burned in with the image's fonts. libplacebo runs on Mesa's CPU Vulkan device, so this needs
# no GPU.
set -euo pipefail

status=0
fail() { echo "FAIL: $*" >&2; status=1; }
ff() { ffmpeg -hide_banner -nostdin -loglevel error "$@"; }
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

hwaccels="$(ffmpeg -hide_banner -hwaccels)"
expected=(vaapi cuda opencl vulkan)
[[ "$(uname -m)" == x86_64 ]] && expected+=(qsv)
for h in "${expected[@]}"; do
  grep -qx "$h" <<<"$hwaccels" || fail "no $h hwaccel"
done
filters="$(ffmpeg -hide_banner -filters)"
for f in tonemapx tonemap_opencl libplacebo zscale subtitles; do
  grep -q " $f " <<<"$filters" || fail "no $f filter"
done
encoders="$(ffmpeg -hide_banner -encoders)"
for e in libx264 libsvtav1 libopus libmp3lame h264_nvenc hevc_nvenc h264_vaapi hevc_vaapi; do
  grep -q " $e " <<<"$encoders" || fail "no $e encoder"
done

src=(-f lavfi -i testsrc2=size=320x240:rate=24:duration=1 -f lavfi -i sine=duration=1)
ff "${src[@]}" -c:v libx264 -c:a libopus "$work/x264.mkv" || fail "libx264 and libopus encode"
ff "${src[@]}" -c:v libsvtav1 -c:a libmp3lame "$work/av1.mkv" || fail "libsvtav1 and libmp3lame encode"
ff -i "$work/av1.mkv" -f null - || fail "AV1 decode"
ff -i "$work/x264.mkv" -vf "setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc,\
format=yuv420p10le,tonemapx=tonemap=bt2390:t=bt709:m=bt709:p=bt709:format=yuv420p" -f null - \
  || fail "tonemapx"
ff -init_hw_device vulkan=vk -filter_hw_device vk -i "$work/x264.mkv" \
  -vf "hwupload,libplacebo=w=160:h=120:format=yuv420p,hwdownload,format=yuv420p" -f null - \
  || fail "libplacebo on Vulkan"
cat >"$work/subs.ass" <<'ASS'
[Script Info]
ScriptType: v4.00+
[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Noto Sans,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:01.00,Default,,0,0,0,,Hello 日本語
ASS
ff -i "$work/x264.mkv" -vf "subtitles=$work/subs.ass" -f null - || fail "subtitles burned in"

[[ $status == 0 ]] && echo "verified $(ffmpeg -hide_banner -version | head -1)"
exit "$status"
