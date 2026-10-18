---
title: "Requirements"
summary: "What kind of machine photon-server runs on, what it needs beside it, and the disk space and network it uses."
---

This page helps you choose a machine and a way to [install](02-install-docker.md) photon-server.
If you plan to use Docker, the Mac app or the Linux desktop app, most of it is taken care of for
you.

## Which machine

| Machine | How to install |
| --- | --- |
| Linux, Intel/AMD or ARM | [Docker](02-install-docker.md), the [Linux desktop app](03-install-natively.md#linux-the-flatpak), or a [download](03-install-natively.md#the-archives) |
| Mac with Apple silicon, macOS 13 or later | The [Mac app](03-install-natively.md#macos-the-mac-app), or a download |
| Mac with an Intel processor | A download |
| Windows 10 or 11, Intel/AMD | The [Windows app](03-install-natively.md#windows-the-windows-app), Docker Desktop, or a download (harder: see below) |

A NAS that can run Docker works the same way as a Linux machine.

## What runs beside it

photon-server keeps its information in two other programs that run next to it:

- **PostgreSQL**, a database. It holds everything that matters: your libraries, profiles, watch
  history and settings. You need version 18 or later.
- **Valkey**, a small, fast store for short-lived things, such as who is watching right now and
  TV sign-in codes. Nothing in it needs to be kept. You need version 9.

Docker, the Mac app and the Linux desktop app run both for you. Only with a download do you install
and run them yourself, as [Install natively](03-install-natively.md#the-archives) describes.

## Video tools

photon-server uses a well-known tool called ffmpeg to read and convert video, and another called
yt-dlp to fetch [theme music](../03-library/11-theme-music.md). Every way of installing it already
includes both, so you do not need to install them.

## Processor and graphics card

Playing a file as it is takes very little effort. Converting video as it plays (transcoding) takes
a lot. Without a graphics card, the server can convert one video at a time for every four
processor threads the machine has.

A graphics card does this far faster. photon-server finds one by itself:

- on a Mac, Apple's built-in video hardware, or an NVIDIA card
- on Linux, an NVIDIA card, or Intel or AMD graphics
- on Windows, an NVIDIA, Intel or AMD card

It tests each one and uses the first that works. If none does, it uses the processor. See
[Transcoding and hardware encoders](../05-playback/01-transcoding.md).

## Disk space

- **Your media** is only ever read. photon-server never changes, moves or deletes your files. It
  only needs permission to read each library's folder.
- **The cache** holds posters, scrubbing previews, videos being converted and downloads being
  prepared. It can grow large. The server refuses to start a new conversion when less than 2 GB is
  free there. See [Storage](../07-maintain/05-storage.md) for where it lives and how to move it.
- **Backups** of the database are kept on the same machine. See
  [Back up and restore](../07-maintain/01-backups.md).

## Network

| Port | What it is for |
| --- | --- |
| 8640 | The web app and every app that connects. This is the one you always need. |
| 8096 | Jellyfin apps, only if you [turn them on](../04-access/05-jellyfin-apps.md). |

Photon finds the server on your home network on port 8640 too. The server only answers devices on
your own network, never the internet. You can turn this off under
Settings › Server › Network › **Discovery**.

The server also needs to reach the internet. It looks up each title's details, posters and
subtitles on sites such as TMDB and TheTVDB, and sends notifications to your devices.

## Browsers and apps

The web app works in any current browser. To watch on a phone, tablet, TV or Mac, see
[Connect Photon and other apps](05-connect-apps.md).

## Good to know

- With a download, the server finds the database and Valkey through two settings, `PHOTON_DATABASE_URL`
  and `PHOTON_VALKEY_URL`. Everything else is set in the web app. See
  [Configuration](../08-reference/01-configuration.md).
- To make and restore backups, the server needs PostgreSQL's own tools (`pg_dump`, `pg_restore` and
  `psql`). Docker and both apps include them. With a download, install them yourself, or the server
  runs but cannot back up.
- If the server's ffmpeg lacks some optional features, it says so in its log as it
  starts. Without them, intros and credits are found from chapters only, and styled subtitles
  cannot be drawn into converted video.
