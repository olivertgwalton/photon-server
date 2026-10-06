#!/bin/sh
# Writes the keyframe fixtures with FFmpeg 9, and prints each one's keyframes as ffprobe walks to them.
set -e
ff() { ffmpeg -v error -y "$@"; }
IN="-f lavfi -i testsrc2=size=64x48:rate=24000/1001:duration=8"
AU="-f lavfi -i sine=duration=8:sample_rate=8000"
X="-c:v libx264 -preset ultrafast -bf 3 -g 300 -sc_threshold 0 -force_key_frames 0,1.3,2.0,4.71,5.1,7.25"
ff $IN $AU $X -c:a aac -b:a 8k -ac 1 cues-end.mkv
ff $IN $X -reserve_index_space 4096 cues-front.mkv
ff $IN $AU -map 1:a -map 0:v $X -c:a aac -b:a 8k -ac 1 -output_ts_offset 1.5 moov-end.mp4
ff $IN $X -movflags +faststart faststart.mov
ff $IN $X -movflags +negative_cts_offsets negative-cts.mp4
ff -f lavfi -i testsrc2=size=64x48:rate=5:duration=3 -c:v mjpeg -q:v 31 intra.mp4
ff $IN $X -movflags +frag_keyframe fragmented.mp4
ff $IN -c:v libvpx-vp9 -deadline realtime -g 30 -cpu-used 8 vp9.webm
ff $IN $X -f matroska - > no-cues.mkv
ff $IN $X transport.ts
for f in *.mkv *.webm *.mp4 *.mov *.ts; do echo "== $f"; ffprobe -v error -select_streams v:0 -show_entries packet=pts_time,flags -of csv=p=0 $f | grep K | cut -d, -f1 | sort -n | tr '\n' ' '; echo; done
