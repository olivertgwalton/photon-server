---
title: "Get started"
summary: "What photon-server is, what it needs, and how to install it, set it up and sign in from your apps."
---

photon-server is a media server for films and television, written in Go and open source under the
GPL-3.0. You point it at folders of films and shows; it matches each title with TMDB and TheTVDB,
fetches its pictures, ratings, subtitles and theme music, and serves the library to a web app it
carries and to [Photon](05-connect-apps.md#photon) on iPhone, iPad, Mac, Apple TV and Apple
Vision Pro. This section takes you from nothing to a server you are watching from.

## What it does

- **Keeps a library of films and shows**: scanned from folders, or from your own NFO files, and
  kept up to date as files arrive. A library can also be streamed by a provider, such as a
  Stremio addon.
- **Plays each file the way the device can.** It sends a file as it is where the device plays it, copies
  its video into another container where only that differs, and transcodes it, on a GPU where
  there is one, where nothing else will do. HDR and Dolby Vision are kept where the device shows
  them.
- **Makes downloads** for watching offline, converted on the server to fit the device.
- **Knows each person**: a profile each, with its own watch history, favourites, watchlist,
  playlists and Home, signed in by password, single sign-on or a code on a TV, and switched to
  with a PIN.
- **Speaks Jellyfin's API**, so apps made for Jellyfin, such as Infuse and Swiftfin, play from it
  too.
- **Connects outwards**: plugins, Stremio addons, Trakt and Simkl, webhooks, notifications, and
  importing watch history from Plex, Jellyfin and Emby.
- **Runs as one server or several** sharing one database, with backups, scheduled tasks and
  Prometheus metrics.

## What it is not

- **Not a music, photo or live TV server.** A library holds films, shows, or both.
- **Not a service.** It runs on your machine, and your profiles, history and settings are in your
  own PostgreSQL. There is no account to make anywhere else.
- **Not a downloader.** It plays what is in your folders, or what a provider you add streams; it
  does not search for or fetch media.

## The path through this section

1. [Requirements](01-requirements.md): the platforms it runs on, PostgreSQL and Valkey, and the
   ports and disk it uses.
2. Install it, one of two ways:
   - [Install with Docker](02-install-docker.md): one Compose file running the server, PostgreSQL
     and Valkey. The way to run it from boot.
   - [Install natively](03-install-natively.md): the Flatpak for Linux, the Mac app, or an archive
     for Linux, macOS or Windows, including running it as a Windows service.
3. [Set up a new server](04-first-run.md): create the admin, name the server, add your first
   libraries and say how it is reached from outside.
4. [Connect Photon and other apps](05-connect-apps.md): sign in from a browser, from Photon, from
   an Apple TV with a code, and from Jellyfin apps.

From there, [Build your library](../03-library/index.md) covers naming files and tuning libraries,
and [Give people access](../04-access/index.md) adds the rest of the household.
