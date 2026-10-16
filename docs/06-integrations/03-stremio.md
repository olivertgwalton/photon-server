---
title: "Stremio addons"
summary: "Register a Stremio addon, make collections of its catalogs, and play what it streams in a remote library."
---

A [Stremio addon](https://github.com/Stremio/stremio-addon-sdk/tree/master/docs) is a web service
that lists catalogs of films and shows and offers streams of them. photon-server registers one as a
plugin, by the address Stremio installs it from, and uses two things of it: its catalogs, as lists
a collection or a library can hold the titles of, and its streams, as the copies a **remote
library** plays. By the end of this page you have an addon registered and a library that plays what
it streams.

An addon describes nothing: the titles it lists are matched and described by the library's own
sources, such as TMDB, as any title is.

## Register an addon

1. Copy the address of the addon's `manifest.json`, as you would to install it in Stremio. For an
   addon you configure, such as one that reaches a debrid service, configure it first: the
   configuration is part of the address.
2. Open Settings › Server › Metadata and find **Register a plugin** under **Plugins**.
3. Change the choice to **Stremio addon**, paste the address, and choose **Register**.

The addon is listed as a **Stremio addon**, with the kinds of title it has and what it answers:
**Lists** where it has a catalog of films or shows, **Streams** where it offers streams.

```sh
curl -X POST https://photon.example/api/v1/admin/plugins \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"url": "https://addon.example/…/manifest.json", "protocol": "stremio"}'
```

The address must be an `http` or `https` URL ending in `/manifest.json`, with no user name,
password or query; any other is refused with "an addon is registered by the address of its
manifest.json". An addon whose types include neither `movie` nor `series` is refused too, as there
is nothing in it the server can use.

### Its id

The addon becomes the provider `plugin:<id>`, its id being its manifest's own made lower case
letters, digits and hyphens: `com.stremio.torrentio.addon` is `plugin:com-stremio-torrentio-addon`.
To install one addon twice, configured apart, give the second an id of its own in the **id
(optional)** field beside the address, or as `id` in the request. An id already registered is
refused with `409`.

### Its address is a secret

An addon's configuration, a debrid service's key among it, is in its address. The server keeps the
address as it keeps a secret setting: the plugin list shows its scheme and host alone, and no error
or log line names the rest. An addon has no settings of its own on its card, as everything it is
told is in that address, and it is told of no events.

To change an addon's configuration, remove it and register it again at its new address.

## Make a collection of a catalog

Each of an addon's catalogs of films (`movie`) or shows (`series`) is a list, named as
`<type>/<catalog id>` as its manifest lists it, such as `movie/top`.

1. Open Settings › Server › Collections.
2. Under **New collection**, name it, set **Titles** to **A … catalog** for the addon, and type
   the catalog's name in **List**.
3. Choose under **Found in** whether the collection holds the catalog's titles from every library
   or from one.

The collection holds the titles of the catalog your libraries have, read again daily. See
[Collections and the lists they follow](../03-library/07-collections.md).

```sh
curl -X POST https://photon.example/api/v1/admin/collections \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"title": "Top films", "list": {"source": "plugin:com-stremio-torrentio-addon", "id": "movie/top"}}'
```

A catalog is read a page at a time, as Stremio pages one, up to 500 titles: a catalog such as
"popular" pages on for as long as it is asked. A title is kept by its IMDb id, or a `tmdb:` id; one
listed by an id of the addon's own, which no other source knows the title by, is left out.

## Play what it streams

A **remote library** has no folder. It holds the titles of a list, or those a search finds, and
plays the copies a provider streams of them, so a household can watch what an addon offers as it
watches its own files.

1. Open Settings › Server › Libraries and choose **Add a library**.
2. Name it and choose what it **Holds**.
3. Set **Its media** to **Streamed by a provider**. This is offered only once a provider that
   streams, such as a Stremio addon, is registered.
4. Under **Titles from**, choose the addon (or any provider that keeps lists) and type the
   catalog in **List**, such as `movie/top`; or choose **No list**.
5. Under **Search finds titles from**, choose a provider that searches, such as TMDB, or
   **Nothing**. A search of the server then shows what it finds that the library does not hold
   yet, the best ten of each library; opened, a title becomes the library's.
6. Under **Streamed by**, choose the addon.
7. Choose **Add and scan**.

A remote library needs a list, a search, or both, and a provider that streams. Once it is made,
its media is fixed.

```sh
curl -X POST https://photon.example/api/v1/admin/libraries \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name": "Streamed films", "kind": "movies", "media": "remote",
       "list": {"source": "plugin:com-stremio-torrentio-addon", "id": "movie/top"},
       "discover": "tmdb", "streams": "plugin:com-stremio-torrentio-addon"}'
```

The server refuses a remote library that names a root, names neither a list nor a search, or names
a provider that does not do what it is asked: "… keeps no lists", "… searches nothing", "… streams
nothing", or "… files no titles under ids of its own" for a search provider whose finds could not
be kept.

### What a scan does

Each scan of a remote library reads its list again. A title the list gains is added, named as the
list names it until it is matched; one the list no longer holds is removed, unless a profile has
played it, favourited it or put it on its watchlist. A list that cannot be read fails the scan
rather than emptying the library. A library of what a search finds alone keeps only what a profile
played, favourited or watchlisted of it.

A remote library has no files, so it cannot be watched for changes, and its titles' files cannot be
deleted from the web app ("a remote library has no folder to watch or delete files from"). Previews
and intros and credits are off for it unless you turn them on.

### How a title plays

When a profile opens a film or an episode, the server asks the addon for its streams: a film by its
IMDb id, else its TMDB id, and an episode by its show's id and its season and episode numbers.
What the addon offers is kept for a minute, so a play and its seeks ask once; each node asks for
its own, as a link may be good only from the address that asked for it.

The server reads the best three of what is offered, in the addon's order:

- A stream that fails is passed over for the next.
- One that does not begin answering within 15 seconds is taken to be **fetching**, as a debrid
  service caching a torrent, or a usenet release being downloaded, keeps a player waiting. The
  player is told "the provider is fetching the title; it plays once it has it", and opening it
  again later plays it.
- One shorter than a minute is refused: an addon answers a stream it could not get with a clip
  saying so.

The first that reads is kept as a copy of the title, known by what its bytes are (a torrent's file,
a file's name and size, or the release it names) rather than its address, which a debrid link signs
afresh each time. So the same stream offered again is the same copy, and a copy the addon no longer
offers is marked missing. Where nothing offered can be read, the player is told "nothing the
provider offers of the title could be read".

A stream with no web address of its own, such as a bare torrent or a YouTube video, and one that
needs headers sent with it, are left out: the server fetches a stream as a player would, and sends
no headers an addon asks for.

## Remove an addon

Remove it as any plugin, from its row under Settings › Server › Metadata. See
[Install and manage plugins](01-plugins.md#remove-a-plugin).
