---
title: "Theme music"
summary: "Theme tunes from files beside your titles or from ThemerrDB, and how a page plays them."
---

A film or a show can have a theme tune that plays quietly under its page, as Plex and Jellyfin
play one. The server finds it in the title's folder, or, where a library asks for it, downloads
the one ThemerrDB lists for the title. This page covers both, and how a profile turns it on.

## Choose where a library's themes come from

Theme music is in a library's settings, at Settings › Server › Libraries, the library, **More
settings**.

| Setting | What it does |
| --- | --- |
| Theme music | **Local files only** plays the files beside a title (the default). **Local files and ThemerrDB's** does too, and downloads ThemerrDB's for a film or show with no file of its own. **None** plays no theme, whatever is beside the title. |

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"themes": "themerr"}'
```

`themes` is `local`, `themerr` or `off`.

## Theme files beside a title

A theme is a sound file in the title's own folder, a film's or a show's, found as the library is
scanned:

- A file named `theme`, such as `theme.mp3`, beside the title.
- Any sound file in a `theme-music` folder beside it, played in order.

```text
Films/Heat (1995)/
  Heat (1995).mkv
  theme.mp3
Shows/Severance/
  theme-music/
    main-title.flac
    season-two.m4a
  Season 01/
```

The formats read are AAC, FLAC, M4A, MP3, Ogg (`.oga`, `.ogg`, `.opus`), WAV and WMA. A season's
and an episode's page play their show's theme. A title with a file of its own never has
ThemerrDB's: the file is what you chose.

## Themes from ThemerrDB

ThemerrDB is a community list of YouTube links to films' and shows' themes, which Jellyfin's
Themerr plugin reads. Under **Local files and ThemerrDB's**, the server looks a title up by its
TMDB id, or a film by its IMDb id where TMDB's finds nothing, and downloads the sound of the link
with yt-dlp, kept as AAC, which browsers and Apple's players both play. A title must be matched
for this (see [fix a title's match](05-fix-a-match.md)).

It needs yt-dlp. The Docker image has it; a native install finds it beside the server or on
`PATH`. Settings › Server › Dashboard says under **yt-dlp** which the server found, and so does
`yt_dlp` in `GET /api/v1/admin/server`. Choosing ThemerrDB's on a server without it is refused.
In a server of several nodes, one without yt-dlp leaves the downloads to one that has it (see
[several servers](../05-playback/04-several-servers.md)).

### When themes are fetched

- Choosing ThemerrDB's for a library asks for a theme for every film and show in it.
- Each time a title is matched, as it is added and whenever its metadata is refreshed, it is
  asked for again, so a theme ThemerrDB has listed or changed since is found. A link fetched
  before is not downloaded again.

Each title's fetch is a job, **Fetch a theme tune**, in Settings › Server › Jobs.

### What is left out, and for how long

- A title ThemerrDB lists no theme for is not asked about again for a day, as Themerr keeps
  ThemerrDB's answers.
- A video YouTube will not give (removed, private, blocked by its rights holder, behind an age
  gate) is not tried again for 7 days, and the server log says why. ThemerrDB replaces a dead link
  with another, which is tried at once.
- A theme larger than 32 MiB is not kept.
- Only YouTube links are followed.

Every node together fetches from YouTube at most once every 10 seconds, so the server is not taken
for a bot, which YouTube stops. A library being filled for the first time takes a while for that.

Downloaded themes are kept with the server's artwork (see [storage](../07-maintain/05-storage.md)).

## Play themes

A theme plays only for a profile that asks for it. At Settings › Playback, turn on **Play theme
music**: a film's or show's theme then plays quietly, once, while you look at its page. It is off
by default.

```sh
curl -X PATCH https://photon.example/api/v1/profile/preferences \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"theme_music": "play"}'
```

A title's page names its themes, in order, as `themes`. Each is at `/api/v1/themes/{id}`, served
in byte ranges to anyone, signed in or not, so a player that sends no headers of its own can play
it. A theme's id changes whenever its file does, so a client may keep one for good.

```sh
curl -o theme.m4a https://photon.example/api/v1/themes/$THEME
```
