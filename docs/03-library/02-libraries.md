---
title: "Add and manage libraries"
summary: "Add a folder as a library, choose how it is read and described, check every node can reach it, and remove it."
---

A library is one folder on the server and everything under it, read as films, shows, or both.
This page adds one, walks through each of its settings, and covers checking, scanning and
removing it. Libraries are kept at Settings › Server › Libraries, which only an admin sees.

## Before you add one

- Lay the folder out as [prepare folders and name files](01-name-your-files.md) describes.
- The path is the path **the server** sees. In Docker that is the path inside the container:
  with the compose file in [install with Docker](../01-get-started/02-install-docker.md), a
  folder `/srv/media/Films` on the host is `/media/Films`.
- The server needs to read the folder, and to write to it only if you allow
  [media deletion](#media-deletion).
- Two libraries cannot share a folder, and two cannot share a name. A file under both of two
  folders, as with a library of `/media` beside one of `/media/Films`, is a title in each.

## Add a library

1. Open Settings › Server › Libraries and choose **Add a library**.
2. Give it a **Name**, as it appears in the sidebar and on Home.
3. Choose what it **Holds**: **Films**, **Shows**, or **Films and shows**. A library of films and
   shows tells a show's folder from a film's by what is in it (see
   [films and shows in one library](01-name-your-files.md#films-and-shows-in-one-library)).
4. Leave **Its media** at **Files in a folder**, and choose the **Folder** from the server's
   folders. Browsing starts from `/`, `/media`, `/mnt`, `/srv`, `/Volumes` and the server's home
   folder, where each is there, and from each drive on Windows.
5. Open **More settings** to change anything below now, or leave the defaults and change them
   later.
6. Choose **Add and scan**.

The server scans the library at once and shows its progress beside it: "Reading folders" as it
walks, then "Forgetting what is gone". Each new film and show is matched to the metadata providers
as it is found, so titles appear before the scan ends and fill in as they are described.

The kind and the folder are fixed once the library is made. To change either, add a new library
and remove the old one.

From the API, a library is added with its name, kind (`movies`, `shows` or `mixed`) and absolute
root, and starts with every default below; change the rest with a `PATCH` after:

```sh
curl -X POST https://photon.example/api/v1/admin/libraries \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Films", "kind": "movies", "root": "/media/Films"}'
```

The answer is `201 Created` with the library, its `id` among it. A root that is not an absolute
path to a folder the server can read is refused.

## Its settings

Every setting is on the library's own page: choose its name, or **Edit**, at Settings › Server ›
Libraries. **Save** applies them.

### Where descriptions and pictures come from

**Metadata downloaders** and **Image fetchers** are ranked lists, one pair for each kind of item
the library holds (films; or shows, seasons and episodes). Tick a source to ask it and move it up
or down to trust it more or less: a lower source fills only what those above it left empty. A new
library asks **Nfo** then **TMDB** for metadata, and **TMDB** for pictures.
[Metadata providers and artwork](03-metadata.md) explains each source and the ranking.

Changing either list identifies every title in the library again.

### Every setting

| Setting | What it does |
| --- | --- |
| **Videos to link from the providers** | The kinds of extra the library keeps links to from its providers, such as a film's trailers on TMDB. Default: Trailer, Featurette and Behind the scenes. |
| **Subtitles to download** | The languages a copy with no subtitle in them has one fetched in, when it is added and daily after. None by default. See [subtitles](10-subtitles.md). |
| **Download** | **Only those made for the file** (the default) or **The best found**, which may be timed for another release. |
| **Watch for changes** | **As they happen** (the default) scans a folder about a minute after its files change; **Only when scanned** waits for the schedule or a scan you start. See [keep libraries up to date](06-keep-up-to-date.md). |
| **Refresh metadata** | How often each film and show is asked about again: **Never**, **Every week**, or every 30 (the default), 60 or 90 days. |
| **Previews** | **Chapters and seeking** (the default), **Chapter images only**, or **None**. See [timeline previews](09-markers-and-previews.md). |
| **Intros and credits** | **From chapters, sound and picture** (the default), **From chapters only**, or **Not looked for**. See [intros and credits](09-markers-and-previews.md). |
| **Keyframes** | **From the file's index** (the default) reads a few small parts of each file, right for a network share; **Read every file through**, in the maintenance window, gives exact segments from a local disk; or **Not looked for**. |
| **Theme music** | **Local files only** (the default), **Local files and ThemerrDB's**, or **None**. ThemerrDB's needs yt-dlp on the server. See [theme music](11-theme-music.md). |
| **Metadata language** | What titles' names, write-ups and pictures are asked for in. **Server default** takes the one at Settings › Server › General. Changing it describes every title again. |
| **Titles** | **In its language** (the default), or **As first named**, which keeps a film's or show's original title and sorts by it, its write-up still in the library's language. |
| **Pictures** | **In its language first** (the default) takes posters and logos in the library's language, else English, else with no words; **The most liked, any language** takes the providers' favourites. |
| **Certification country** | Whose certificates titles carry, which parental controls read. **Automatic** is the language's country, else the server's. |
| **Media deletion** | See below. |

A single film or show can be given its own language, country and titles over its library's: see
[fix a title](05-fix-a-match.md#a-titles-own-language).

### Media deletion

**Media deletion** is **Not allowed** by default. **Allowed, files and all** lets an admin's
**Delete…** on a title remove its files from the disk, a show's or season's episodes with it, and
then the title, and each folder that leaves empty. The server must be able to write to the folder, and deleted files cannot be
brought back. Should one file fail to delete, the title stays, and the files already deleted are
missing at the next scan.

### From the API

`PATCH` changes only what it sends. This turns off watching and asks for metadata in British
English:

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"monitor": "off", "metadata_language": "en-GB"}'
```

`refresh_days` is from 0, never, to 365. `metadata_language` is an IETF tag and
`certification_country` an ISO 3166-1 code, `""` for the server's own; the languages and countries
offered are at `GET /api/v1/admin/locales`. `themes: "themerr"` is refused on a server without
yt-dlp. What a new library of each kind ranks, and what it may, is at
`GET /api/v1/admin/library-kinds`.

## Check the server can read it

**Check access** on a library asks every node running to read its folder, and says what each
found: readable with how many entries, or not there, not a folder, not allowed to read it, can't be
read, or didn't answer in time, as a hung network mount does. A folder that is readable but empty
usually means the media is not mounted. With [several servers](../05-playback/04-several-servers.md),
every node reads the libraries from its own mount, at the same path.

```sh
curl -X POST https://photon.example/api/v1/admin/libraries/$LIBRARY/check \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Scan and refresh

- **Scan now** reads the library's folders for what is new, changed or gone. **Scan all
  libraries**, on Settings › Server, scans every one.
- **Refresh metadata** asks the providers about the library's titles again. **Refresh missing
  metadata** takes only films and shows never matched, with no overview or poster, with a new
  season, or that failed last time; **Refresh all metadata** takes every title, season and
  episode, and on a large library can take hours. Your edits, held fields and chosen artwork stay
  either way.

Both are also on the library's menu in the sidebar. [Keep libraries up to date](06-keep-up-to-date.md)
covers scanning in full.

## Remove a library

**Remove** forgets the library's titles and what everyone has watched of them. The files on disk
are not touched.

```sh
curl -X DELETE https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## A library streamed by a provider

**Its media** offers a second kind of library: **Streamed by a provider**, a library with no
folder whose titles are a list's, or what a search finds, played from what a provider streams, such
as a Stremio addon. It is offered once a provider that streams is registered at
Settings › Server › Metadata. Choose:

- **Titles from**: a provider that keeps lists, and the **List**'s id there, such as `movie/top`;
  or **No list**.
- **Search finds titles from**: TMDB, TheTVDB or a plugin whose finds a search of the server shows;
  a title found becomes the library's once it is opened. **Nothing** keeps the library to its list.
- **Streamed by**: the provider whose streams play its titles. A title's streams are asked for as
  it is opened, and the best that plays is kept as its copy.

A streamed library has no folder to watch and nothing to delete, and makes no previews or markers
by default. A scan reads its list again; a list that cannot be read fails the scan rather than
emptying the library. [Stremio addons](../06-integrations/03-stremio.md) sets one up end to end.
