---
title: "Install and manage plugins"
summary: "Register a plugin by its address, set it up, rank it in a library's sources, and remove it."
---

A plugin is a web service of its own that photon-server calls over HTTP. Once registered it is a
provider like the built-in TMDB, TheTVDB and MDBList: it takes settings such as an API key, a
library can take metadata and pictures from it in the order you rank it, and whatever else it
answers (ratings, people, lists, streams, subtitles, intros and credits, pages of its own) is used
where the server uses those.

This page is for the admin running the server. To write a plugin, see
[Write a plugin](02-write-a-plugin.md). A Stremio addon is registered the same way and has a page
of its own: [Stremio addons](03-stremio.md).

## Register a plugin

1. Run the plugin somewhere the server can reach it over HTTP. It need not be public: an address
   on the server's own network, or a container's name on the same Docker network, is enough.
2. Open Settings › Server › Metadata and find **Plugins** below the built-in providers.
3. Under **Register a plugin**, leave the choice at **Photon plugin**, type the address the plugin
   answers at, such as `http://plugin:8080`, and choose **Register**.

The server reads `GET <address>/manifest` and keeps what it says. The plugin is then listed with
its name, its address, the protocol version it speaks, the kinds of title it knows (films, shows,
or both) and what it answers, and it appears among the providers above as `plugin:<id>`, where
`<id>` is the id its manifest gives.

```sh
curl -X POST https://photon.example/api/v1/admin/plugins \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"url": "http://plugin:8080"}'
```

### Why a plugin is refused

The server checks the address and the manifest before it keeps anything, and answers `400` with
the reason:

- the address is not an `http` or `https` URL, carries a user name or password ("credentials go
  in the plugin's settings, not its address"), or has a query or fragment;
- the plugin speaks another version of the protocol than the server does;
- its id is not lower case letters, digits and hyphens, it has no name, or it names no kinds, or a
  kind other than `movie` and `show`;
- it answers no capability at a version the server speaks;
- a setting has no name, or is listed twice.

A plugin whose id is already registered is answered `409`. One that does not answer, or answers a
`5xx`, is answered `502` (`provider_unavailable`), with what went wrong but never the value of a
secret setting.

The server follows no redirect from a plugin, the manifest's included. It calls only the address
the plugin was registered at, so a plugin cannot send it, carrying the plugin's settings, to
another host on your network.

## Set it up

A plugin that declares settings has them on its card under Settings › Server › Metadata, as a
built-in provider does. The card says **Ready** once every setting marked **(required)** is set,
and **Needs settings** until then; a plugin whose required settings are not all set is never
called.

A secret setting, such as an API key, is never shown again once saved: its field reads "Set; type
to replace it". Leave it empty to keep it, type to replace it, or choose **Clear** to remove it.
The server never writes a secret into its log or into an error.

```sh
curl -X PATCH https://photon.example/api/v1/admin/providers/plugin:films \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"settings": {"api_key": "…"}}'
```

## Give it a place in a library

A plugin that describes titles, or has pictures of them, takes part in a library only once you
rank it there. Open the library under Settings › Server › Libraries and find **Metadata
downloaders** and **Image fetchers** for each kind of title it holds. Turn the plugin on and drag
it to where it belongs: a source lower down is asked only for what those above it did not say.
Changing these identifies every title in the library again.

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$FILMS_ID \
  -H "Authorization: Bearer $ADMIN_TOKEN" -d '{"sources": [{"kind": "movie",
    "metadata": [{"source": "nfo", "enabled": true}, {"source": "plugin:films", "enabled": true}, {"source": "tmdb", "enabled": true}],
    "images": [{"source": "plugin:films", "enabled": true}, {"source": "tmdb", "enabled": true}]}]}'
```

Ranking a source no registered plugin has is refused: "no plugin registered has that id".

What the other capabilities are used for, as the plugin's card lists them:

| Capability | Where the server uses it |
|---|---|
| Describes titles | Matching and describing a library's titles, where the library ranks it. |
| Searches, to fix a match | Listed when an admin fixes a title's match by hand, and as **Search finds titles from** for a remote library. See [Fix a title's match](../03-library/05-fix-a-match.md). |
| Ratings | Scores from the sites it names, beside other providers'. |
| People | What is known of the people a title credits. |
| Lists | A collection's titles, and the titles of a remote library. See [Collections](../03-library/07-collections.md). |
| Streams | The copies a remote library plays. See [Stremio addons](03-stremio.md#play-what-it-streams). |
| Subtitles | Subtitles offered beside other providers', and fetched for titles with none. See [Subtitles](../03-library/10-subtitles.md). |
| Events | Told of what happens on the server, as a webhook is. See [Webhooks](05-webhooks.md). |
| Intros and credits | Timings for intros and credits, asked as markers are found. See [Intros, credits and previews](../03-library/09-markers-and-previews.md). |
| Pages | Pages of its own, linked from the web app's sidebar. |

### Pages in the sidebar

A plugin with pages adds a **Plugins** group to the web app's sidebar, holding each page a profile
may open: a page the manifest marks for admins appears to admins alone, and one for everyone to
every profile. Opening one opens the plugin's own site, never the server's, so nothing on the page
can read a profile's session there. The plugin is told who opened it by a one-time code, as
[Write a plugin](02-write-a-plugin.md#pages) describes.

## When a plugin is down

Each call to a plugin has 20 seconds. A plugin that does not answer, times out or answers a `5xx`
is passed over: the server asks the library's next source, as it does a provider whose key is not
set, so a plugin being down never empties a library. A `4xx` answer to describing a title fails
that title's job, which shows under Settings › Server › Jobs and can be tried again there; to a
rating or a person, the title or person goes without.

## Read its manifest again

A plugin that has learnt a new capability or setting says so only in its manifest. Choose **Read
manifest again** on its row, and the server reads it as it did when you registered it, with the
same checks. A plugin may not change its id: one whose manifest now gives another is refused
("it was registered as … and now calls itself …"), and stays as it was.

```sh
curl -X POST https://photon.example/api/v1/admin/plugins/films/refresh \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Remove a plugin

Choose **Remove** on its row and confirm. The server forgets the plugin, its settings and its
place in every library's sources, and stops telling it of events.

What the plugin said about titles stays as it is, ranked below every other source, so the next
source to say anything of a field replaces it; its pictures come after every other source's, and
a library that ranked them chooses each title's best picture again. This is so that removing a
plugin never leaves titles bare while the other sources catch up.

```sh
curl -X DELETE https://photon.example/api/v1/admin/plugins/films \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

A plugin that calls the server itself does so with an API key you made for it. Removing the plugin
does not revoke that key: revoke it under Settings › Server › API keys.

| Route | |
|---|---|
| `GET /api/v1/admin/plugins` | the registered plugins |
| `POST /api/v1/admin/plugins` | register the plugin at `url` |
| `POST /api/v1/admin/plugins/{slug}/refresh` | read its manifest again |
| `DELETE /api/v1/admin/plugins/{slug}` | forget it |
| `PATCH /api/v1/admin/providers/{id}` | change its settings, as `plugin:{slug}` |

The [API reference](../08-reference/03-api.md) lists every field.
