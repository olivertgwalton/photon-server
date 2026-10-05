# Corresponding Source for photon-server's FFmpeg

The `ffmpeg` and `ffprobe` executables shipped with photon-server are licensed GPL-3.0-or-later.
This archive is their complete Corresponding Source:

- `ffmpeg-<tag>.tar`: FFmpeg at the release tag the executables were built from.
- `FFmpeg-Builds-<commit>.tar`: the BtbN/FFmpeg-Builds scripts, at the commit used, that
  configure and build FFmpeg and every library below.
- `libraries/`: the source of every library, as the build fetched it, one archive per build
  stage.

To rebuild: unpack `FFmpeg-Builds`, place the `libraries/` archives in its `.cache/downloads/`,
and run `./makeimage.sh <target> gpl <ver>` followed by
`GIT_BRANCH_OVERRIDE=<tag> ./build.sh <target> gpl <ver>`.
