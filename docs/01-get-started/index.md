---
title: "Get started"
summary: "What photon-server is, what it needs, and how to install it, set it up and sign in from your apps."
---

photon-server is a media server for your films and TV shows. You point it at the folders where
they live. It recognises each title, fetches its poster, description, ratings and subtitles, and
lets you watch from a browser or from [Photon](05-connect-apps.md#photon) on iPhone, iPad, Mac,
Apple TV and Apple Vision Pro. It is free and open source.

## New here? Start with the quick start

[Quick start](00-quick-start.md) takes about 15 minutes. It goes from nothing to watching a film
on your Apple TV, with a link to the full page at each step. Come back to the pages below when you
want more detail.

## What it does

- **Keeps your library tidy.** It reads your folders, matches each film and episode, and notices
  new files as they arrive.
- **Plays on any device.** When a device can play a file as it is, photon-server sends it
  unchanged. When it can't, the server converts it as it plays (this is called transcoding), using
  your graphics card if you have one. HDR (the brighter, richer picture on newer TVs) is kept
  wherever the screen can show it.
- **Makes downloads** you can watch offline, sized to fit the device.
- **Knows each person.** Everyone in the household gets a profile with their own history,
  favourites, watchlist and Home page. A TV signs in with a short code, and a profile can be
  locked with a PIN.
- **Works with Jellyfin apps** too, such as Infuse and Swiftfin.
- **Connects to other services**, such as Trakt, Simkl and AniList, and can import your watch
  history from Plex, Jellyfin and Emby.

## What it is not

- **Not for music, photos or live TV.** A library holds films, shows, or both.
- **Not an online service.** It runs on your own machine. Your profiles and history stay there,
  and there is no account to make anywhere else.
- **Not a downloader.** It plays what is already in your folders. It does not search for or fetch
  films.

## The full path

1. [Requirements](01-requirements.md): what kind of machine it runs on, and what it needs.
2. Install it, one of two ways:
   - [Install with Docker](02-install-docker.md): the way to keep it running on a home server, a
     NAS or a PC that is always on.
   - [Install natively](03-install-natively.md): the Mac app, the Linux desktop app, or a download
     for Linux, macOS or Windows.
3. [Set up a new server](04-first-run.md): create your admin profile, name the server and add your
   first libraries.
4. [Connect Photon and other apps](05-connect-apps.md): sign in from a browser, from Photon, from
   an Apple TV, and from Jellyfin apps.

After that, [Build your library](../03-library/index.md) covers naming your files and tuning your
libraries, and [Give people access](../04-access/index.md) adds the rest of the household.
