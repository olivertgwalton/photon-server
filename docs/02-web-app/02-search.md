---
title: "Search"
summary: "Find a film, show, episode, collection or person by any word of its name, and narrow the results to one library."
---

The search field sits at the top of every page. Type and press Return: the results page lists what
matches, grouped by kind, with people last.

## What a search finds

A search matches **the start of words**, ignoring case and accents. Every word you type has to
start a word of the title, in any order: `amel` finds Amélie, and `dark knight` finds The Dark
Knight Rises. A title's original title counts too, so a film can be found by the name it was
released under.

Titles are ranked:

1. One named exactly what you typed.
2. Those whose name starts with it.
3. The rest, by how closely they match.

Episodes come after the films, shows and collections that match as well, so a show is not buried
under its own episodes.

Where nothing has the words you typed and you typed at least five letters or digits, the server
looks again for titles with a letter wrong, the nearest first: `intersteller` still finds
Interstellar. Under five, too many titles share a few letters for that to mean anything, so a short
typo finds nothing.

**People** are matched the same way, by the start of each word of their name: those whose name
starts with what you typed first, then those credited on the most titles.

A search only ever finds what your profile may see, and a title held in several libraries once.

## The results page

The results come in rows, each headed by its kind: **Films**, **Shows**, **Episodes**,
**Collections**, then **People**. The page reads the first 100 titles found; a row's name opens the
whole of its kind, from the first 200.

Where you can see more than one library, **Search in** narrows the results to one library, or
**Every library**. A collection is found in a library when it holds a title of that library. People
are not narrowed: a person is not in a library.

Nothing found says so: "Nothing here matches" what you typed.

A search of every library may also list, at the end of its first page, titles a remote library's provider
finds that the server does not hold yet, such as a [Stremio addon's](../06-integrations/03-stremio.md).
Opening one adds it to its library.

## Search from the API

`GET /api/v1/search` takes the text as `q`, and pages titles and people side by side:

```sh
curl "https://photon.example/api/v1/search?q=dark%20knight&limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

- `library` narrows it to one library's titles.
- `kind` asks for only some kinds: `movie`, `show`, `collection`, `episode` or `person`, repeated
  or comma-separated. Everything by default.
- `offset` and `limit` page it, up to 200 at once.

The answer holds `titles` and `people`, each with its `items` and its `total`.
