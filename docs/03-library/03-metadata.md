---
title: "Metadata providers and artwork"
summary: "The providers photon-server matches and describes titles with, their keys, and how a library ranks them for descriptions, ratings and pictures."
---

A file's name says what a title is; a metadata provider says the rest: its write-up, cast, genres,
certificate, ratings and pictures. This page covers the providers photon-server has built in, the
keys they take, how a library chooses and ranks them, and how a title is matched to one.

## The providers

| Provider | Describes | Gives |
| --- | --- | --- |
| **TMDB** | Films and shows | Names, write-ups, dates, genres, studios, certificates, cast and crew, box sets, trailers and other videos, its users' score, posters, backdrops, logos and episode stills. Its charts are lists a collection can follow. |
| **TheTVDB** | Shows | The same for shows, seasons and episodes, numbered as aired, as on the DVDs, or absolutely. |
| **MDBList** | Films and shows | Ratings: IMDb's, TMDB's, and Rotten Tomatoes' critics and audience. Its lists are lists a collection can follow. |
| **The Open Movie Database** | Films and shows | Names, plots, certificates, genres, a poster, episodes' names and plots, and IMDb's and Rotten Tomatoes' scores. |

OpenSubtitles is listed beside them and finds subtitles only: see [subtitles](10-subtitles.md).
A plugin or a Stremio addon you register is a provider like these, with what its manifest says it
does: see [plugins](../06-integrations/01-plugins.md).

## Set a provider's keys

Providers are at Settings › Server › Metadata. Each card says what the provider describes, what it
can do, and **Ready** or **Needs settings**.

| Provider | Setting | What it does |
| --- | --- | --- |
| TMDB | **API read access token** | Optional. photon-server ships with the project's own token, as Jellyfin ships its key, so matching works without an account; your own replaces it. |
| TheTVDB | **API key** | Optional. The project's own key is used where none is set. |
| TheTVDB | **Subscriber PIN** | For a key subscribers pay for, the subscriber's PIN. |
| MDBList | **API key** (required) | Each server uses its own free key from [MDBList](https://mdblist.com/preferences/). |
| The Open Movie Database | **API key** (required) | Each server uses its own key from [OMDb](https://www.omdbapi.com/apikey.aspx). |

A secret is never shown again once saved: its field reads "Set; type to replace it", and
**Clear API key** (or the setting's own name) removes it. TMDB's token is its "API Read Access Token" on
[its API settings](https://www.themoviedb.org/settings/api), not the shorter API key.

A provider that needs its settings and is still asked by a library is listed under **Needs
attention** on Settings › Server until it has them. Until then it is passed over: the next source
is asked and no title fails.

```sh
curl -X PATCH https://photon.example/api/v1/admin/providers/mdblist \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"settings": {"api_key": "your-key"}}'
```

The setting keys are TMDB's `token`, TheTVDB's `key` and `pin`, and MDBList's and OMDb's
`api_key`; `""` clears one. `GET /api/v1/admin/providers` lists every provider, what is set and
whether it is ready.

## How a library ranks its sources

Each library ranks, in its settings, where it takes each kind of item's metadata and pictures
from: **Metadata downloaders** and **Image fetchers**, once for films and, in a library of shows,
once each for shows, seasons and episodes. Tick a source to ask it, and order them most trusted
first.

A value is written by the highest-ranked source that gives one: a lower source fills only what
those above it left empty. A list, such as genres, is taken whole from one source, so two
providers' genres never stand side by side. Beyond the ranking:

- What you write on a title outranks every source, and stays through every refresh (see
  [edit details](05-fix-a-match.md#edit-its-details)).
- What the file and folder names say ranks lowest: a title is called by its file only until a
  source names it.
- A source left unticked keeps its place but is not asked.

A new library asks **Nfo**, the [NFO files](04-nfo.md) beside your media, then **TMDB** for
metadata, and **TMDB** for pictures. Each source can be ranked only for what it holds:

| Source | Metadata of | Pictures of |
| --- | --- | --- |
| Nfo | everything | nothing |
| TMDB | everything | everything |
| TheTVDB | shows, seasons, episodes | shows, seasons, episodes |
| MDBList | films and shows | nothing |
| The Open Movie Database | everything but seasons | films and shows |

So to describe a show's episodes from TheTVDB rather than TMDB, tick TheTVDB under **Metadata
downloaders (Episodes)** and move it above TMDB. Changing a ranking identifies every title in the
library again.

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"sources": [{"kind": "episode", "metadata": [
        {"source": "nfo", "enabled": true},
        {"source": "tvdb", "enabled": true},
        {"source": "tmdb", "enabled": true}]}]}'
```

A kind or a list left out of `sources` is left as it is. A plugin is ranked by its source id,
`plugin:` and its slug.

## How a title is matched

When a scan finds a new film or show, the server asks each provider the library ranks, in turn,
to match it:

1. **By an id it already carries**: from its folder or file name (`[tmdbid-949]`), or its NFO.
2. **By another provider's id**: TMDB finds a title by its IMDb or TheTVDB id, TheTVDB by its IMDb
   or TMDB id. TMDB is asked first, so the ids it gives lead TheTVDB and OMDb to the same title.
3. **By a search** for the name and year. A result is taken only where its title, or original
   title, is the one asked for, ignoring case, accents and punctuation, and its year is within
   one of the one asked for. With no result at all, the name is searched again without its year,
   as a folder's year is often one of several release dates.

A title with no confident match is left as its files and NFO describe it, rather than risk a wrong
match, and the log says "no confident match". Fix it by hand: see
[fix a match](05-fix-a-match.md). OMDb is asked by IMDb id alone, as Jellyfin asks it, so it says
nothing of a title no other source found an IMDb id for.

A provider that cannot be reached, or lacks a setting it needs, is passed over and the next one
asked.

## Ratings

A title's ratings come from every source the library asks for its metadata that gives them: TMDB
its own users' score, MDBList IMDb's, TMDB's and Rotten Tomatoes', OMDb IMDb's and Rotten
Tomatoes'. Where two give the same site's, the one ranked higher stands. To show Rotten Tomatoes
on cards, tick MDBList or OMDb for films' and shows' metadata. A rater that fails, as one past its
daily allowance does, is noted in the log and the title is described all the same.

## Pictures

A title's pictures of each kind are ranked:

1. The one you chose (see [choose artwork](05-fix-a-match.md#choose-artwork)).
2. Files beside the title (see [pictures beside a title](01-name-your-files.md#pictures-beside-a-title)).
3. The providers the library asks for pictures, in its order.

The library's **Pictures** setting chooses among a provider's own: **In its language first** takes
posters and logos in the library's language, else English, else with no words on them, and
backdrops with no words first; **The most liked, any language** takes the provider's favourites.

Pictures are fetched into the server's picture cache and served from there at the size each
client asks.

## Languages and certificates

A library asks its providers in its **Metadata language** and takes certificates of its
**Certification country**; **Server default** and **Automatic** follow Settings › Server ›
General. **Titles** set to **As first named** keeps each film's and show's original title. A title
can take a language of its own over its library's: see
[a title's own language](05-fix-a-match.md#a-titles-own-language).

## Seasons and episodes

TMDB numbers a show's episodes as they aired. A show whose files are numbered as on the DVDs, or
absolutely, is described without its seasons by TMDB: set its
[episode order](05-fix-a-match.md#episode-order) and rank TheTVDB for its seasons and episodes,
which numbers them in any of the three orders.

A show with an episode announced to air is matched again every 7 days, whatever its library's
**Refresh metadata** says, so its calendar keeps up with its dates.
