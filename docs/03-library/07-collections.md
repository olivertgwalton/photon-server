---
title: "Collections and the lists they follow"
summary: "Box sets made by TMDB, by hand, by a library's filters, or from a list kept on TMDB, MDBList or a plugin."
---

A collection is a box set of titles. It belongs to no library: it is listed in the Collections
view of its own, and among the collections of every library whose titles it holds. This page
covers the four kinds, how each is made and kept up to date, and what an admin can change.

Every profile sees a collection's titles as it may see them, so a profile limited to some
libraries or certificates finds only those of a collection's titles it is allowed. A collection
with no poster of its own is drawn from its titles' posters: the one, or a mosaic of the first
four, as Plex draws one.

## The four kinds

| Kind | Made by | Its titles |
| --- | --- | --- |
| From TMDB | The server, as it matches films | The films TMDB names part of the box set, earliest released first |
| Made here | An admin, by hand | Films, shows, seasons and episodes of any library, in the order given |
| Smart | An admin, from a library's filters | What the filters find, found again as the library changes |
| From a list | An admin, from a list kept on a provider | The titles of the list the chosen libraries hold, in the list's order |

Settings › Server › Collections lists every collection with a badge for its kind, and opens each
one.

## Collections from TMDB

As TMDB matches a film it names the box set the film belongs to, and the server puts the film in
that collection, making the collection the first time. A film TMDB stops naming part of a set
leaves it at the next match.

A collection from TMDB is shown once it holds two titles: a box set of one is no set. It follows
TMDB, so its titles are not edited here. You can rename it or choose its artwork, as for any
title (see [fix a title's match and edit its details](05-fix-a-match.md)).

## Make a collection by hand

1. Open Settings › Server › Collections.
2. Type a name under **New collection**, leave **Titles** at **By hand**, and choose **Make it**.
3. On the collection's page, use **Add titles** to find a film, show, season or episode, and
   **Add** each one.
4. Move titles with their arrows, take one out with its cross, and choose **Save the order**.
   Added titles are kept only once the order is saved.

A title can also be put in one from its own page: the title's More menu (⋯) › Admin › **Add to
collection…** lists the collections made here and can make a new one. An extra or another
collection cannot be put in a collection.

```sh
curl -X POST https://photon.example/api/v1/admin/collections \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title": "Christmas films"}'
```

`POST /api/v1/admin/collections/{id}/members` adds titles at the end, each given once and left
where it is if already there; `PUT` on the same path replaces them all, in the order given.

```sh
curl -X PUT https://photon.example/api/v1/admin/collections/$COLLECTION/members \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"item_ids": ["0199…", "0199…"]}'
```

## Save a library's filters as a smart collection

A smart collection is a filtered library wall kept by name, as Plex's are.

1. Open a library and narrow it with its filters, its sort, or both.
2. Choose **Save as a smart collection**, give it a name, and choose **Save**.

It holds what those filters find, in that order, and the server finds them again after each scan
of the library and every 15 minutes, so a title matched, refreshed or edited since is caught.
To change the filters, open the collection's page, choose **Change its filters**, adjust the wall
it opens, and choose **Save to the collection**.

A smart collection is the same for everyone, so its filters cannot read one profile's marks
(watched, unwatched and the like) or sort by when it was played, and a wall showing only one kind
of a library's items, such as its episodes, cannot be saved. The web app hides the button for
those.

The rule is the wall's filter: `genres`, `years`, `certificates`, `studios`, `resolutions`,
`ranges`, `people`, `starts_with`, and `min_rating` out of 100 on `rating_site` (IMDb where none is
named). `sort` is `title`, `added`, `released`, `rating` or `runtime`, in title order where none is
given. `limit` keeps only the first so many, and is set through the API alone.

```sh
curl -X POST https://photon.example/api/v1/admin/collections \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Recent comedies",
    "library_ids": ["0198…"],
    "rule": {"filter": {"genres": ["Comedy"], "years": [2025, 2026]}, "sort": "released", "order": "desc", "limit": 50}
  }'
```

`PUT /api/v1/admin/collections/{id}/rule` replaces the rule and finds its titles again at once.

## Follow a list

A list collection holds the titles of a list kept elsewhere that your libraries have, as Kometa's
list builders do. It reads the list again every day.

1. Open Settings › Server › Collections.
2. Type a name under **New collection**.
3. Set **Titles** to **A TMDB list**, **An MDBList list**, or a plugin's catalog.
4. Type the list under **List**, and choose where its titles are looked for under **Found in**:
   **Every library**, or one.
5. Choose **Make it**.

Or choose **Browse lists** to pick one: TMDB's charts and MDBList's best liked lists, or those a
search names. A list picked there is looked for in every library and named as the list is.

What **List** takes:

- **TMDB**: a list's number, such as `8136`, or one of TMDB's charts by its path, such as
  `movie/top_rated` or `trending/tv/week`. Of a chart, its first 100 titles are read.
- **MDBList**: `user/list`, or the list's id. MDBList also mirrors lists from Trakt and IMDb. It
  needs its API key set under Settings › Server › Metadata.
- **A plugin's catalog**: the id the plugin gives it, such as a Stremio addon's catalog (see
  [Stremio addons](../06-integrations/03-stremio.md)).

Each title of the list is looked for by its TMDB id, then its IMDb id, among the films or shows
of the libraries chosen, and is kept once, where the list first has it. The collection's page
says how many of the list's titles are in none of those libraries; they are counted rather than
shown.

The scheduled task **Sync list collections** reads every list again daily. A list that cannot be
read leaves its collection as it was, so a provider being down never empties one. **Sync now** on
the collection's page reads it at once.

```sh
curl -X POST https://photon.example/api/v1/admin/collections \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title": "Top rated films", "library_ids": ["0198…"], "list": {"source": "tmdb", "id": "movie/top_rated"}}'
```

`GET /api/v1/admin/lists?source=mdblist&query=oscars` answers the lists a provider offers, and
`POST /api/v1/admin/collections/{id}/sync` reads one collection's list now.

## Remove a collection

On the collection's page, choose **Remove this collection**. Its titles stay in their libraries.
A collection made here, smart or from a list can be removed; one from TMDB cannot, as it follows
TMDB, which alone changes it.

```sh
curl -X DELETE https://photon.example/api/v1/admin/collections/$COLLECTION \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Scheduled tasks

| Task | When | What it does |
| --- | --- | --- |
| Refresh smart collections | Every 15 minutes, and after each scan for that library | Finds every smart collection's titles again |
| Sync list collections | Every day | Reads each list collection's list again |

Both can be run at once from Settings › Server › Scheduled tasks (see
[scheduled tasks and jobs](../07-maintain/04-tasks-and-jobs.md)).
