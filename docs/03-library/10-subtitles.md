---
title: "Subtitles and subtitle providers"
summary: "Subtitles inside and beside your files, finding more on OpenSubtitles or a plugin, and fetching missing ones by themselves."
---

A copy's subtitles are the tracks inside its file and the subtitle files beside it, both read as
the library is scanned. Where a copy has none in a language you want, the server can find one on
a subtitle provider: when someone asks for it from the title, or by itself for every copy that
lacks one. This page covers all three.

## Subtitles inside and beside a file

The scan reads every subtitle track inside a file, with its language and whether it is forced,
for the hearing impaired or the default.

A subtitle file beside a video is named after the video, with tags before its extension:

```text
Heat (1995).mkv
Heat (1995).en.srt
Heat (1995).en.sdh.srt
Heat (1995).en.forced.srt
Heat (1995).Director's Commentary.fr.default.ass
```

- A language is a two- or three-letter code (`en`, `fre`) or its English name (`English`).
- `forced` or `foreign` marks a forced subtitle; `sdh`, `cc`, or `hi` beside a language marks one
  for the hearing impaired (`hi` alone is Hindi); `default` marks the default.
- Anything else is its title.
- A `Subs` or `Subtitles` folder beside the video is read as if its files were beside it.

The formats read are SubRip (`.srt`), ASS and SSA, WebVTT (`.vtt`), PGS (`.sup`), VobSub
(`.idx` with its `.sub`) and SAMI (`.smi`, `.sami`). See
[prepare folders and name files](01-name-your-files.md) for naming the videos themselves.

Which subtitle comes on is each profile's choice, at Settings › Playback (see
[your settings](../02-web-app/06-your-settings.md)).

## Set up a subtitle provider

The server finds subtitles on OpenSubtitles.com, and on any plugin with the Subtitles capability
(see [plugins](../06-integrations/01-plugins.md)). OpenSubtitles needs an account, and each
server uses a consumer key of its own.

1. Sign in at [opensubtitles.com](https://www.opensubtitles.com), and make a consumer under its
   API consumers page.
2. Open Settings › Server › Metadata and find **OpenSubtitles**.
3. Fill in **Consumer API key**, **Username** and **Password**, all three required, and choose
   **Save**.

The card says **Ready** once it has what it needs.

```sh
curl -X PATCH https://photon.example/api/v1/admin/providers/opensubtitles \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"settings": {"api_key": "…", "username": "…", "password": "…"}}'
```

A search uses the consumer key alone. A download signs in as the account and counts against its
downloads for the day; once they are used, the server stops fetching until the next day.

## Find a subtitle for one title

1. Open a film or an episode and choose its More menu (⋯) › **Find subtitles…**.
2. The search starts in the profile's subtitle language, else the browser's. Choose another
   language to search again.
3. Choose **Add** beside one.

Results made for this very file come first, starred, then the most downloaded. Each says whether
it is for the hearing impaired (SDH) or forced.

Anyone signed in can do this, for the titles they can see. What they add is kept beside the copy
for every profile, as Jellyfin's subtitle download does.

```sh
curl "https://photon.example/api/v1/titles/$TITLE/subtitles/candidates?language=fr" \
  -H "Authorization: Bearer $TOKEN"
```

```sh
curl -X POST https://photon.example/api/v1/titles/$TITLE/subtitles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"version_id": "0199…", "source": "opensubtitles", "id": "8521347", "language": "fr"}'
```

The search answers the copy it searched as `version_id`: the copy asked for with `version_id`, or
the title's longest.

### How a search finds a match

- A film is searched by its IMDb id, else its TMDB id; an episode by its show's ids and its
  season and episode numbers. A title with neither id finds nothing, so a title must be matched first (see
  [fix a title's match](05-fix-a-match.md)).
- A copy that is one file is searched by its hash too, which is how a subtitle made for the very
  file is told apart. A copy in several parts is searched without one, as one file of several is
  no release on its own.
- A subtitle in several files, for a release in several parts, is left out.
- Portuguese and Chinese are told apart by where they are spoken and written: `pt-BR` is
  Brazilian, any other `pt` European; `zh-Hant`, `zh-TW` and `zh-HK` are Traditional, any other
  `zh` Simplified.

A fetched subtitle is kept as SubRip in the database, so every node can serve it, and written to
the node's cache the first time it is served from there.

## Fetch missing subtitles by themselves

A library can fetch a subtitle for every copy that has none in the languages you name, as
Jellyfin's "Download missing subtitles" does. Both settings are in the library's settings, at
Settings › Server › Libraries, the library, **More settings**, under **Subtitles to download**.

| Setting | What it does |
| --- | --- |
| Languages | Tick each language to fetch. None are ticked by default, which fetches nothing. |
| Download | **Only those made for the file** (the default) takes only a subtitle found by the file's hash. **The best found** takes the best of any, which may be timed for another release of the title. |

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"subtitle_languages": ["en", "fr"], "subtitle_match": "release"}'
```

Languages are BCP 47 tags such as `fr` or `pt-BR`; `[]` stops fetching.

The scheduled task **Download missing subtitles** runs daily, and after a scan that added titles
to a library that fetches any, so a new title has its subtitles the same day. It works through
the newest copies first, and for each copy and language:

- A copy counts as having a language when it has a subtitle in it, inside or beside it, in any of
  its regions. A forced subtitle alone does not count: it covers only what is not in the copy's
  own language.
- A forced subtitle is never fetched by itself.
- A copy searched with nothing to be had is not searched again in that language for 7 days. Most
  subtitles are written in a release's first weeks, and searching daily would spend the
  provider's requests on titles that have none.
- The task stops for the day when the account's downloads run out, and carries on at its next
  run.

**Run now** on the task, at Settings › Server › Scheduled tasks, runs it at once:

```sh
curl -X POST https://photon.example/api/v1/admin/tasks/fetch_subtitles/run \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Remove a fetched subtitle

A subtitle the server fetched can be forgotten by its id, which the title's page lists among each
version's `subtitles`. One that is a file in a library is the library's, and is not removed this
way: delete or rename the file and scan.

```sh
curl -X DELETE https://photon.example/api/v1/admin/subtitles/$SUBTITLE \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```
