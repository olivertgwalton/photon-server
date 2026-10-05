# Corresponding Source for photon-server's FFmpeg

The `ffmpeg` and `ffprobe` executables shipped with photon-server are licensed GPL-3.0-or-later.
This archive is their complete Corresponding Source:

- `sources/`: FFmpeg and every library linked into it, each as the archive the build fetched,
  listed with its origin in `sources.txt` and its checksum in `sources.sha256`.
- `build.sh`: the script that configures and builds them, cross-compiling with Zig.

To rebuild, with Zig, CMake, Meson, Ninja, NASM, autoconf, automake, libtool and gperf installed:
place `sources/*` in `.work/downloads/` beside `build.sh` and run `./build.sh linux64 OUT` or
`./build.sh linuxarm64 OUT`.
