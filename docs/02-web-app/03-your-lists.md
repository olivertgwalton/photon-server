---
title: "Your lists"
summary: "Keep a watchlist and favourites, read back what you have played, and see what is coming out on the calendar."
---

Each profile keeps its own lists: what it means to watch, what it likes, what it has played and
what it has watched. They are the profile's on every device and in every app that signs in as it,
and nobody else in the household sees them.

## What counts as watched

The server decides how far a play got from where it stopped:

- Stopped in the first 5%, it is not kept: a peek at the start is not somewhere to resume from,
  and moves nothing up Next Up.
- Stopped between 5% and 90%, it is in Continue Watching, to resume from there.
- Past 90%, within a second of the end, or anywhere past the start of a title under five minutes,
  it is **watched**, and counted as a play once however often a player reports from the end.

A show or a season is watched once every episode of it is. Marking a show or a season watched or
unwatched marks each of its episodes; marking a collection marks each title in it.

To mark a title by hand, use the tick on its page or **Mark as watched** in a card's menu. **Mark as
unwatched** also forgets where you stopped. **Remove from Continue Watching** forgets where you
stopped and keeps whether you watched it.

```sh
curl -X PUT https://photon.example/api/v1/titles/$TITLE/watched \
  -H "Authorization: Bearer $TOKEN"
```

`DELETE` on the same address marks it unwatched; `DELETE /api/v1/titles/{id}/progress` takes it out
of Continue Watching.

## Watchlist

The watchlist is what you mean to watch. Add a title with the bookmark on its page, or **Add to
watchlist** in a card's menu; **Watchlist** in the sidebar shows it, the latest added first.

- Only films and shows go on it. A season or an episode puts its show on, and taking either off
  takes its show off.
- A collection or an extra cannot go on it.
- **Watching takes a title off**: a film once it is watched, a show once every episode of it is.
  The watchlist is for what is still to watch, so a finished title leaves it by itself.

```sh
curl -X PUT https://photon.example/api/v1/titles/$TITLE/watchlist \
  -H "Authorization: Bearer $TOKEN"
```

## Favourites

Favourites are what you like, kept for good: watching a title leaves it there. Add one with the
heart on its page, or **Add to favourites** in a card's menu. Any title can be a favourite, a
collection or a single episode included.

**Favourites** in the sidebar shows them grouped by kind, **Films**, **Shows**, **Seasons**, **Episodes**,
**Collections** and **Extras**, up to 200 of them, the latest added first.

```sh
curl -X PUT https://photon.example/api/v1/titles/$TITLE/favourite \
  -H "Authorization: Bearer $TOKEN"
```

Both lists are also Home rows, which can be moved or hidden; see
[Home rows](06-your-settings.md#home-rows).

A title held in several libraries is one title on your lists: putting it on the watchlist in one
library puts it there in all of them, and a mark, a favourite or where you stopped is the same
wherever you open it.

## History

**History** lists everything you have played, by the day it started, the latest first. Each play
says when it started, where it **stopped at**, and how it reached you: **Direct play** (the file as
it is), **Direct stream** (the same picture in another container) or **Transcode** (converted by
the server).

```sh
curl "https://photon.example/api/v1/history?limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

An admin reads everyone's plays under Settings › Server › Play history; see
[signed-in devices and what is playing now](../04-access/07-devices-and-playback.md). Plays from
Plex, Jellyfin or Emby can be [imported](../06-integrations/07-import-history.md).

## Calendar

**Calendar** shows the films released and the episodes airing day by day, including episodes your
libraries do not have yet. It opens on **Upcoming**: six weeks from yesterday, each day that has
anything on it headed **Yesterday**, **Today**, **Tomorrow** or by date. **Month** draws a month's
grid, Monday first, with the previous and next month and **Today** to come back; on a phone the
month is listed day by day.

Its filters choose whose titles:

| Filter | What it shows |
| --- | --- |
| My titles | The default: films and shows you have started, put on your watchlist or made a favourite. |
| Watchlist | What is on your watchlist. |
| Favourites | Your favourites. |
| Everything | Every film and show your profile sees. |

A season or an episode on a list stands for its show, so a show you have begun brings every new
episode of it.

Each entry leads to its title. An episode the server has no file for is greyed out and badged
**Unaired** until its day has passed, then **Missing**; it leads to its show. Episodes are badged
**Series premiere**, **Season premiere** or **Finale** where they are one, and anything you have
watched is dimmed.

```sh
curl "https://photon.example/api/v1/calendar?start=2026-10-01&end=2026-11-11&filter=mine" \
  -H "Authorization: Bearer $TOKEN"
```

The server answers at most six weeks at once. Episodes not yet here come from the show's
metadata provider; see [metadata providers](../03-library/03-metadata.md).
