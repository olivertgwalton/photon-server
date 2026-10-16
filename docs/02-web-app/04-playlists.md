---
title: "Playlists"
summary: "Make your own lists of films and episodes, put them in order, and play them through or shuffled."
---

A playlist is a list of films and episodes you put together and play in your order, or shuffled. A
playlist is your profile's own: nobody else in the household sees it, and every app that signs in
as you does. Collections, by contrast, are the server's, kept by an admin for everyone; see
[collections](../03-library/07-collections.md).

## Make a playlist

1. Open **Playlists** in the sidebar.
2. Type a name under **New playlist** and choose **Create**.

The new playlist opens, empty. You can also make one while adding a title: **Add to playlist…**
offers **New playlist** under your existing ones, and the title goes straight into it.

**Playlists** lists yours by name, each with how many titles it holds and how long they run.

```sh
curl -X POST https://photon.example/api/v1/playlists \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Friday night", "item_ids": ["…"]}'
```

`item_ids` is optional: leave it out for an empty playlist.

## Add titles

Choose **Add to playlist…** from a card's **⋯** menu or from **⋯** on a title's page, then the
playlist. The dialog lists your playlists with how many titles each holds.

What goes in is what plays:

- A film or an episode goes in as it is.
- A show goes in as all its episodes, in order, specials last.
- A season goes in as its episodes, in order.
- A collection goes in as its titles, each film as it is and each show or season as its episodes.
- An extra cannot be added.

Titles go at the end, in the order given. A title already in the playlist can go in again: each
entry is its own.

```sh
curl -X POST https://photon.example/api/v1/playlists/$PLAYLIST/entries \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"item_ids": ["…"]}'
```

## Order, rename and delete

On a playlist's page:

- The up and down arrows beside an entry move it one place.
- The cross beside it takes it out of the playlist.
- **Rename** changes the name; **Save** keeps it.
- **Delete** removes the playlist, after asking. Its titles stay in the library.

To move an entry from the API, give its `entry_id` (from the playlist's entries) and the place,
counted from 0; a place past the end puts it last:

```sh
curl -X PUT https://photon.example/api/v1/playlists/$PLAYLIST/entries/$ENTRY/position \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"position": 0}'
```

An entry for a title your profile can no longer see, such as one in a library an admin has taken
from you, is passed over: it is not listed and not played, and it comes back if you are given the
library again.

## Play a playlist

**Play all** plays the playlist from its first entry; **Shuffle** plays every entry once, in a
random order. Each title plays in the [player](05-watching.md) as any other does, and when one
ends the next follows, offered by **Up next** as the credits roll (straight away, or after a count,
by [Play the next episode](06-your-settings.md#playback)). **Previous** and **Next** in the player,
and Shift+P and Shift+N, step through it.

The order being played belongs to that browser tab. Reloading the page, or opening a title from
elsewhere, leaves the playlist: the player then goes on as it does after any title, to the next
episode of a show.

What you watch from a playlist is marked watched and kept in Continue Watching as any play is: a
playlist keeps no place of its own.
