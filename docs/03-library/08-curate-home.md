---
title: "Curate Home"
summary: "What Home's rows hold, how each profile orders and hides them, and what an admin shapes for everyone."
---

Home is a page of rows. Each profile arranges its own: which rows it shows, and in what order,
kept on the server so every device the profile signs in on draws the same Home. This page says
what each row holds, how a profile arranges them, and what an admin decides that reaches every
profile's Home.

## The rows

There are eight. Four are the profile's own; the other four are made once for each library of
the kinds they suit, as Plex's library rows are, and headed with the library's name.

| Row | Made for | What it holds |
| --- | --- | --- |
| Continue Watching | The profile | Films and episodes stopped part way, the most recently played first |
| Next Up | The profile | For each show under way, the episode after the furthest one watched, specials aside; shows in the order last played |
| Watchlist | The profile | What the profile put on its watchlist, the latest first |
| Favourites | The profile | What the profile marked a favourite, the latest first |
| Recently Added Films | Each film or mixed library: "Recently Added in Films" | Films, the last added first |
| Recently Added Shows | Each show or mixed library: "Recently Added in Shows" | Shows, by their newest episode added |
| Recently Released | Every library: "Recently Released in Films" | Films released, and shows with an episode aired, in the past year, the newest first; nothing yet to come |
| Top Rated | Every library: "Top Rated in Films" | Films and shows the profile has not begun, by their IMDb rating, leaving out a rating with fewer than 1,000 votes |

A few rules hold for every row:

- The web app draws 20 titles of a row. A library's row is no more than its newest or best;
  a profile's own row is paged through to its end.
- A row holds only what the profile may see, so a profile limited to some libraries or
  certificates gets rows of those alone.
- A title held in two libraries appears once in the profile's own rows, and once in each
  library's row.
- An episode under way is in Continue Watching, so Next Up leaves it out.

## The order a profile sees

A new profile sees all eight rows, in the order of the table above. No row starts hidden.

The library rows follow the profile's order of its libraries: a profile that puts Shows above
Films in the sidebar gets "Recently Added in Shows" first wherever the Recently Added rows sit.
The order is changed from a library's menu in the sidebar, **Reorder**, then dragging, or with
`PUT /api/v1/profile/library-order`.

## Arrange a profile's Home

Each profile arranges its own Home at Settings › Home:

1. Drag a row by its grip, or move it with its arrows.
2. Turn a row's switch off to hide it. A hidden row is not drawn, nor asked for.

Each change is kept at once. Every Photon app and the web app read the same arrangement, so it
follows the profile to every device. See [your settings](../02-web-app/06-your-settings.md) for
the rest of a profile's own settings.

The arrangement is the `home` of the profile's preferences, every row in the order drawn with
whether it is `shown` or `hidden`. A change sends the rows in the order wanted; any left out
follow, shown.

```sh
curl -X PATCH https://photon.example/api/v1/profile/preferences \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"home": [
    {"row": "continue_watching", "visibility": "shown"},
    {"row": "recently_added_shows", "visibility": "shown"},
    {"row": "next_up", "visibility": "shown"},
    {"row": "top_rated_unwatched", "visibility": "hidden"}
  ]}'
```

The rows are `continue_watching`, `next_up`, `watchlist`, `favourites`, `recently_added_films`,
`recently_added_shows`, `recently_released` and `top_rated_unwatched`. The preferences answer
`library_kinds` beside each library row: the kinds of library it is made once for each of.

A row the server gains in a later version is shown at the foot of a Home arranged before it, so
an arrangement never hides something new by accident.

## Read a row

A client asks for each row on its own, so one slow row holds up no other:

- A profile's row: `GET /api/v1/profile/rows/{row}`, a page of it.
- A library's row: `GET /api/v1/libraries/{id}/rows/{row}`, its first 20, or `limit` of them.

```sh
curl "https://photon.example/api/v1/libraries/$LIBRARY/rows/recently_added_films?limit=20" \
  -H "Authorization: Bearer $TOKEN"
```

## What an admin shapes

An admin does not arrange another profile's Home; each profile keeps its own. What an admin
decides reaches every Home all the same:

- **Which libraries a profile sees.** A library a profile cannot open has no rows on its Home,
  and its titles are in none of the profile's rows. Set this on the profile (see
  [profiles](../04-access/01-profiles.md)).
- **What kind of library it is.** A library's kind decides which rows it gets: a
  film library has no Recently Added Shows row. See [add and manage libraries](02-libraries.md).
- **The ratings Top Rated reads.** It reads IMDb's rating, so a library whose titles have none
  from the metadata providers has an empty Top Rated row. See
  [metadata providers and artwork](03-metadata.md).
- **The library's name.** Library rows are headed with it.

Collections and playlists are not Home rows. A collection is reached from the Collections view
and from each library it holds titles of (see
[collections and the lists they follow](07-collections.md)).
