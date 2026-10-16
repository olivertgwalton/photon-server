---
title: "Requirements"
summary: "What photon-server runs on, what it needs beside it, and the ports and disk it uses."
---

This page lists what a machine needs to run photon-server, so you can choose how to
[install it](02-install-docker.md) before you start.

## Platforms

| Platform | Install with |
| --- | --- |
| Linux, amd64 or arm64 | [Docker](02-install-docker.md), the [Flatpak](03-install-natively.md#linux-the-flatpak) or an [archive](03-install-natively.md#the-archives) |
| macOS 13 or later, Apple silicon | The [Mac app](03-install-natively.md#macos-the-mac-app), or an archive |
| macOS, Intel | An archive |
| Windows, amd64 | Docker Desktop, or an archive with Valkey run in WSL or Docker |

A NAS that runs Docker containers runs the image as any Linux machine does.

## PostgreSQL and Valkey

photon-server keeps its state in two services beside it. The Compose file, the Flatpak and the Mac
app run both for you; with an archive, you run them.

- **PostgreSQL 18 or later.** The server refuses an older one as it starts. It holds everything
  durable: libraries, titles, profiles, watch history, settings. The server needs a database and
  a user that owns it.
- **The PostgreSQL client tools**, `pg_dump`, `pg_restore` and `psql`, no older than the server
  they reach, found beside photon-server's binary or on its `PATH`. They make and restore
  [backups](../07-maintain/01-backups.md); without them the server runs but does neither.
- **Valkey 9.** It holds what is live and can be lost: signed-in sessions, playbacks, the jobs
  queue and what each node tells the others. The server uses Valkey 9's expiry of single hash
  fields, which older versions lack. 256 MB of memory is what the Compose file gives it.

The server's keys in Valkey all begin `photon:` and its server id, so one Valkey can be shared; a
Valkey user limited to keys matching `photon:*`, as the Compose file makes, is enough. Allow it
`INFO` too, or Settings › Server › Dashboard cannot show Valkey's version.

The server finds both from two environment variables, which every install sets for you except an
archive:

| Variable | What it does |
| --- | --- |
| `PHOTON_DATABASE_URL` | PostgreSQL's address, such as `postgres://photon:<password>@localhost/photon`. Required. |
| `PHOTON_VALKEY_URL` | Valkey's address, such as `valkey://localhost:6379`, or `unix:///path/to/valkey.sock`. Required. |

The rest of the server's configuration lives in its database and is set in the web app. See
[Configuration](../08-reference/01-configuration.md).

## Media tools

The server runs ffmpeg and ffprobe to read and convert video, and yt-dlp to fetch
[theme music](../03-library/11-theme-music.md). The image, the Flatpak, the Mac app and every
archive carry all three: jellyfin-ffmpeg 8.1, with VAAPI, Quick Sync, NVENC, VideoToolbox and
Vulkan, and yt-dlp's standalone build. The server looks for each beside its own binary first,
then on the `PATH`, and logs which it found as it starts (`"msg":"media tools"`).

Two things depend on how ffmpeg was built, and the server warns as it starts where its ffmpeg
lacks them:

- Without the `chromaprint` muxer, [intros and credits](../03-library/09-markers-and-previews.md)
  are found from chapters alone.
- Without libass, styled subtitles play only in apps that draw them themselves; the server cannot
  draw them into a transcode.

## Processor and GPU

A transcode in software is given four logical CPUs, so a server encoding in software runs one
for every four it has, at least one. Playing a file as it is, or copying its video into a
different container, takes very little.

The server encodes on hardware where it can, trying in order:

| Machine | Encoders tried |
| --- | --- |
| Mac | VideoToolbox, then NVENC |
| Linux | NVENC, then Quick Sync and VAAPI on each `/dev/dri/renderD*` device |
| Windows | NVENC, then Quick Sync, then AMF |

It takes the first that encodes a test picture, and software where none does. A hardware encoder
runs up to eight transcodes at once unless an admin sets a limit. HEVC is encoded where the
encoder makes it, and H.264 alone otherwise. See
[Transcoding and hardware encoders](../05-playback/01-transcoding.md).

## Disk

- **Media** is read, never written: the server needs read access to each library's folder, and
  nothing more. Each node must see the media at the same path.
- **The cache** holds pictures, timeline previews, transcodes in progress and converted
  downloads. A node refuses a new transcode when less than 2 GiB is free in it. It lives in the
  user's cache folder, `photon-server` within it: `/var/cache/photon-server` in the image,
  `~/.cache/photon-server` on Linux, `~/Library/Caches/photon-server` on a Mac,
  `%LOCALAPPDATA%\photon-server` on Windows. Pictures and previews can be moved to an object
  bucket; see [Storage](../07-maintain/05-storage.md).
- **Backups** are database dumps, kept in `photon-server/backups` in the user's config folder:
  `/var/lib/photon-server/backups` in the image, `~/.config/photon-server/backups` on Linux,
  `~/Library/Application Support/photon-server/backups` on a Mac, and beside the cache on
  Windows, as its config folder roams with the user's profile.

## Network

| Port | Protocol | What it is |
| --- | --- | --- |
| 8640 | TCP | The web app and the API, over HTTP, or HTTPS where [secure connections](../04-access/04-remote-access.md) are on. Fixed. |
| 8640 | UDP | Discovery: answers apps on the local network looking for a server. |
| 8096 | TCP | The [Jellyfin API](../04-access/05-jellyfin-apps.md), where an admin turns it on. The port can be changed. |

Discovery answers only devices on the same machine or a private network, and can be turned off
under Settings › Server › Network › Discovery.

The server reaches out to the metadata providers (TMDB, TheTVDB, OMDb, MDBList), to the subtitle
and theme music sources, and to any plugins and Stremio addons you add, so it needs to reach the
internet to match titles and fetch their pictures, subtitles and theme music.

## Browsers and apps

The web app runs in a current browser. To play from a phone, tablet, TV or Mac app, see
[Connect Photon and other apps](05-connect-apps.md).
