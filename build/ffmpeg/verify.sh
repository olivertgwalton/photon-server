#!/usr/bin/env bash
# Runs a built ffmpeg on Linux and fails unless it is the build the server expects: every feature
# BUILDCONF lists configured, each hardware path present, and each library actually encoding or
# decoding. Usage: verify.sh DIR, where DIR holds ffmpeg, ffprobe and BUILDCONF.
set -euo pipefail

dir="$(cd "${1:?usage: verify.sh DIR}" && pwd)"
ff() { "$dir/ffmpeg" -hide_banner -nostdin -loglevel error "$@"; }
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
status=0
fail() { echo "FAIL: $*" >&2; status=1; }

conf="$("$dir/ffmpeg" -hide_banner -buildconf)"
while read -r flag; do
  grep -qe "^ *$flag\$" <<<"$conf" || fail "not configured with $flag"
done <"$dir/BUILDCONF"
grep -qe "--enable-nonfree" <<<"$conf" && fail "configured nonfree: not redistributable"

hwaccels="$("$dir/ffmpeg" -hide_banner -hwaccels)"
expected=(vaapi cuda opencl)
grep -qe "--enable-libvpl" "$dir/BUILDCONF" && expected+=(qsv)
for h in "${expected[@]}"; do
  grep -qx "$h" <<<"$hwaccels" || fail "no $h hwaccel"
done
for e in h264_nvenc hevc_nvenc av1_nvenc h264_vaapi hevc_vaapi; do
  "$dir/ffmpeg" -hide_banner -encoders | grep -q " $e " || fail "no $e encoder"
done
"$dir/ffmpeg" -hide_banner -protocols | grep -qx "  https" || fail "no https protocol"

src=(-f lavfi -i testsrc2=size=320x240:rate=24:duration=1 -f lavfi -i sine=duration=1)
ff "${src[@]}" -c:v libx264 -c:a libopus "$work/x264.mkv" || fail "libx264 and libopus encode"
ff "${src[@]}" -c:v libsvtav1 -c:a libmp3lame "$work/av1.mkv" || fail "libsvtav1 and libmp3lame encode"
ff -c:v libdav1d -i "$work/av1.mkv" -f null - || fail "libdav1d decode"
# The way a tone map runs: to linear light and back, the source's colour stated, as zimg needs it.
ff -i "$work/x264.mkv" -vf "zscale=tin=bt709:min=bt709:pin=bt709:rin=tv:t=linear:npl=100,format=gbrpf32le,\
zscale=p=bt709:t=bt709:m=bt709:r=tv,format=yuv420p" -f null - || fail "zimg"
cat >"$work/subs.ass" <<'ASS'
[Script Info]
ScriptType: v4.00+
[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Sans,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:01.00,Default,,0,0,0,,Hello
ASS
ff -i "$work/x264.mkv" -vf "subtitles=$work/subs.ass" -f null - || fail "libass subtitles"
"$dir/ffprobe" -v error -show_entries stream=codec_name -of csv=p=0 "$work/av1.mkv" | grep -qx av1 \
  || fail "ffprobe"

[[ $status == 0 ]] && echo "verified $("$dir/ffmpeg" -hide_banner -version | head -1)"
exit "$status"
