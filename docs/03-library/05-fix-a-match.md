---
title: "Fix a title's match and edit its details"
summary: "Match a title to the right film or show by hand, edit and hold its fields, choose its pictures, set its episode order and language, and refresh it."
---

The scanner matches most titles by their names alone. When it matches one to the wrong film, finds
no match, or a provider describes it in a way you would rather not show, this page puts it right.
Everything here is for an admin.

## Where to find it

Each title's **More** menu, the button on its card and on its page, holds what an admin can do to
it: **Edit…**, **Fix match…**, **Unmatch**, **Split apart…**, **Choose artwork…**, **Refresh
metadata…**, **Analyse**, **Manage…** and **Delete…**, as far as each applies to the title.
**Edit…**, **Fix match…** and **Choose artwork…** open one dialog with **Details**, **Match** and
**Artwork** tabs. **Manage…** opens the title's own page under Settings › Server, with everything
below on one page, with its [markers](09-markers-and-previews.md) beside them.

A title's id, which every call below takes, is the last part of its page's address:
`/titles/0199…`.

## Fix a match

1. Open **Fix match…** on the film or show. The **Match** tab says what it is matched to now, by
   each provider's id, or "Not matched to any provider".
2. Choose the **Provider** to search, change the **Name** and **Year** if the file's are wrong, and
   choose **Search**.
3. Each result shows its poster, name, year and write-up, to tell like-named titles apart. Choose
   **Select** beside the right one.

The title is matched again to that provider's title, and its details follow in a moment. The id
you choose outranks any the file name or NFO gives, and the ids the old match led to are dropped,
so every other provider finds the title again by the new one. Only a film or a show is matched; a
season or an episode follows its show.

Only providers that search and are ready are offered: TMDB for films and shows, TheTVDB for
shows, and plugins that search.

```sh
curl "https://photon.example/api/v1/admin/titles/$TITLE/candidates?provider=tmdb&title=Heat&year=1995" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

curl -X PUT https://photon.example/api/v1/admin/titles/$TITLE/match \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"provider": "tmdb", "id": "949"}'
```

`provider` in the match is `tmdb`, `imdb`, `tvdb` or a plugin's source id, so a title can be
pinned to an IMDb id you already know without searching.

To have the scanner match a title right from the start, put the id in its folder's name: see
[naming the match yourself](01-name-your-files.md#naming-the-match-yourself).

### Unmatch

**Unmatch** takes a film or show off its providers: it keeps what its files and NFO say until its
match is fixed or it is refreshed. Use it for a home video or a recording no provider knows,
rather than leave it matched to a stranger's film.

```sh
curl -X DELETE https://photon.example/api/v1/admin/titles/$TITLE/match \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Edit its details

The **Details** tab edits a title's **Title**, **Sort title**, **Original title**, **Tagline**,
**Overview**, **Certificate**, **Released**, **Year**, **Genres** and **Studios** (genres and
studios separated by commas). Seasons, episodes and extras are edited the same way.

- What you write outranks every source and lasts through every refresh.
- **Hold** beside a field keeps it as it is now without writing it: no source changes it, as a
  locked field in Jellyfin.
- **Give back** returns a field to its sources at the title's next match, and **Give every field
  back** returns them all.
- A field cannot be emptied, only given back. **Sort title** sorts by the title where it is empty.

```sh
curl -X PATCH https://photon.example/api/v1/admin/titles/$TITLE \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"sort_title": "Godfather 2", "locked": ["genres"]}'

curl -X DELETE "https://photon.example/api/v1/admin/titles/$TITLE/edits?field=sort_title" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The fields are `title`, `sort_title`, `original_title`, `overview`, `tagline`, `certificate`,
`release_date`, `year`, `genres` and `studios`. `DELETE …/edits` with no `field` gives back every
one.

## Choose artwork

The **Artwork** tab has a tab for each kind of picture: **Poster**, **Backdrop**, **Still**,
**Logo** and **Banner**. Each lists the pictures every provider has of that kind, with its source,
language and size; choose one to use it over every source. **Give the poster back to the sources**
(or the kind shown) undoes the choice.

To use a picture of your own, put it beside the title, named as
[pictures beside a title](01-name-your-files.md#pictures-beside-a-title) says. A file beside the
title is used before any provider's, and the next scan of its folder finds it.

```sh
curl "https://photon.example/api/v1/admin/titles/$TITLE/artwork/candidates?kind=poster" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

curl -X PUT https://photon.example/api/v1/admin/titles/$TITLE/artwork/poster \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id": "0199…"}'
```

## A title's own language

A film or show can be described in a language other than its library's: the **Language** card on
its Manage page, or under **Details** in the dialog. **Metadata language**, **Certification
country** and **Title** (**In its language** or **As first named**) each default to **Its
library's**. **Save language** describes the title again in them, its seasons and episodes with it.
So one film in a library described in English can be described in Japanese.

```sh
curl -X PUT https://photon.example/api/v1/admin/titles/$TITLE/locale \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata_language": "ja", "certification_country": "", "title_language": "original"}'
```

`""` is the library's own.

## Episode order

A show's files may be numbered as the episodes aired, as on the DVDs, or absolutely, counting
every episode from the first as anime often is. The **Episode order** card on the show's Manage
page sets which: **Numbered as** **Aired** (the default), **DVD** or **Absolute**, then **Save
order**.

Changing it drops what any provider said of the show's seasons and episodes and matches every one
again in the new order; the episodes keep their old titles until then. What files and NFOs say, and
your edits and chosen pictures, stand. TMDB numbers only as aired, so for DVD or absolute order
rank TheTVDB for the library's seasons and episodes (see
[seasons and episodes](03-metadata.md#seasons-and-episodes)).

```sh
curl -X PUT https://photon.example/api/v1/admin/titles/$SHOW/episode-order \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"order": "absolute"}'
```

## Split a film apart

A film the scanner read as several versions of one, when they are different films, is split with
**Split apart…**. The copy that plays first stays; each other becomes a film of its own, named as
its file is and matched afresh, and stays so through every scan, unlike Plex's. It is offered on a
film with more than one copy.

```sh
curl -X POST https://photon.example/api/v1/admin/titles/$TITLE/split \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Refresh a title

**Refresh metadata…** asks a title's providers about it again now, without waiting for its
library's schedule. **Refresh missing metadata** updates the title and fills in any season or
episode with no details yet; **Refresh all metadata** fetches everything again, every season and
episode of a show included. Your edits, held fields and chosen artwork are kept either way. A
season or an episode is refreshed as its show.

```sh
curl -X POST https://photon.example/api/v1/admin/titles/$TITLE/refresh \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode": "all"}'
```

**Analyse** is a different thing: it reads the title's files again, as Plex's Analyze does, and
remakes what is made from them, such as its keyframes, previews and markers.

## Delete a title

**Delete…** deletes a title's files from the disk, a show's or season's episodes with it, and the
title with them, where its library allows [media deletion](02-libraries.md#media-deletion). The
files cannot be brought back. A file you delete by hand instead leaves its title with a missing
copy: see [keep libraries up to date](06-keep-up-to-date.md#a-file-that-moves-or-goes-missing).
