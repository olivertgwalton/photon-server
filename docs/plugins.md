# Plugins

A plugin is a web service that tells photon-server about films and shows, keeps lists of them, or
streams them. It can be written in any language and run anywhere the server can reach over HTTP.
An admin registers it by its address; from then on it is a provider like the built-in TMDB,
TheTVDB and MDBList: it is listed among the providers, takes settings such as an API key, and a
library can take metadata from it, ranked where the admin puts it, or play the streams it offers
for the titles of a list.

This is protocol version **1**.

## Registering one

```sh
curl -X POST https://photon.example/api/v1/admin/plugins \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"url": "http://films-plugin.local:9000"}'
```

The server reads `GET {url}/manifest` and refuses a plugin whose protocol it does not speak, whose
manifest is not valid, or whose id is already registered. The plugin is then the provider
`plugin:{id}`:

```sh
# its settings, as the manifest declares them
curl -X PATCH https://photon.example/api/v1/admin/providers/plugin:films \
  -H "Authorization: Bearer $ADMIN_TOKEN" -d '{"settings": {"api_key": "…"}}'

# a library that takes its metadata after any NFO beside the files and before TMDB, and its pictures
curl -X PATCH https://photon.example/api/v1/admin/libraries/$FILMS_ID \
  -H "Authorization: Bearer $ADMIN_TOKEN" -d '{"sources": [{"kind": "movie",
    "metadata": [{"source": "nfo", "enabled": true}, {"source": "plugin:films", "enabled": true}, {"source": "tmdb", "enabled": true}],
    "images": [{"source": "plugin:films", "enabled": true}, {"source": "tmdb", "enabled": true}]}]}'
```

| Route | |
|---|---|
| `GET /api/v1/admin/plugins` | the registered plugins |
| `POST /api/v1/admin/plugins` | register the plugin at `{"url": "…"}` |
| `POST /api/v1/admin/plugins/{id}/refresh` | read its manifest again, after it gains a capability or a setting |
| `DELETE /api/v1/admin/plugins/{id}` | forget it, its settings and its place in each library's sources |

Removing a plugin keeps what it said about titles as it is, ranked below every source a library
takes, so the next source to speak of a field replaces it; its pictures come after every other
source's.

## Calls

Every call but the manifest is a `POST` of a JSON body, answered `200 OK` with a JSON body.

- **Settings.** Every request carries `settings`: what the admin set for the plugin, by key. A
  plugin is not called at all while a setting it marks `required` is unset. Secrets are never
  shown to admins again once set, and never logged by the server.
- **Locale.** A match, describe, search or person request carries `language`, an IETF tag such
  as `en-GB` that its words are wanted in, and `country`, an ISO 3166-1 alpha-2 code such as `GB`
  whose certificates are wanted: the title's library's, or the server's own. Either may be absent.
  `artwork` is `localized` for pictures in that language first, or `any` for the most liked.
- **Ids** are maps from a provider to the title's or person's id there: `imdb`, `tmdb`, `tvdb`,
  and the plugin's own, `plugin:{id}`, once it has given one.
- **Kinds** are `movie` and `show`. A plugin is asked only about the kinds its manifest names.
- **Errors.** Answer a `4xx` or `5xx` status, with an RFC 9457 problem (`application/problem+json`,
  `{"title": "…", "detail": "…"}`) where there is something to say. A `5xx`, a timeout or no
  answer at all is a plugin that is down: the server passes over it to the library's next source,
  as it does a provider whose key is not set. A `4xx` from a `describe` call fails the
  title's job, which an admin sees and can retry; from a `rate` or `person` call it is logged and
  the title or person goes without.
- **Limits.** Each call has 20 seconds and may answer at most 8 MiB. A redirect is not followed:
  it is answered as a refusal, as the server asks only the address the plugin was registered at.
- Anything the server has no name for (a kind of picture, a rating site, a credit kind) is left
  out, as is a field left out of the answer. A picture is a `http` or `https` URL the server
  fetches and caches itself, as it does TMDB's.

### `GET /manifest`

```json
{
  "protocol": 1,
  "id": "films",
  "name": "Films Database",
  "kinds": ["movie", "show"],
  "capabilities": [
    {"name": "describe", "version": 1},
    {"name": "search", "version": 1},
    {"name": "rate", "version": 1},
    {"name": "person", "version": 1},
    {"name": "list", "version": 1},
    {"name": "stream", "version": 1},
    {"name": "subtitles", "version": 1}
  ],
  "settings": [
    {"key": "api_key", "name": "API key", "secret": true, "required": true},
    {"key": "region", "name": "Region"}
  ]
}
```

- `id` is the plugin's slug: lower case letters, digits and hyphens, starting with a letter or
  digit, at most 63 characters. It may not change between manifests.
- `capabilities` are what the plugin answers, each at a version of it. A capability's calls are
  `POST /{name}/v{version}/{call}`: `describe` is `match` and `describe`, `search` is `search`,
  `rate` is `ratings`, `person` is `person`, `list` is `list`, `stream` is `streams`,
  `subtitles` is `search` and `fetch`. Leave a capability out and it is never called.
- A capability the server does not speak, by name or at that version, is passed over, and a
  plugin that answers none it speaks is refused. So a plugin can name one at two versions, or one
  only a newer server speaks, and be registered by any server for what that server speaks.

### `POST /describe/v1/match`

Find the title the server has. Where `ids` carries the plugin's own id, given before or pinned by
an admin, answer it. Answer an empty `id` when there is no confident match: a wrong match is worse
than none.

```json
{
  "settings": {"api_key": "…"},
  "language": "en-GB",
  "country": "GB",
  "kind": "movie",
  "title": "Jaws",
  "year": 1975,
  "ids": {"imdb": "tt0073195"}
}
```

```json
{"id": "jaws-1"}
```

### `POST /describe/v1/describe`

Say what is known of the title matched. For a show, `seasons` are the seasons the server wants,
numbered in `order` (`aired`, `dvd` or `absolute`); describe those you have. A `season_scope` of
`every`, which a show in a remote library has no files to number seasons by is asked with, wants
every season you have; `numbered`, or none, wants those in `seasons`.

```json
{
  "settings": {"api_key": "…"},
  "language": "en-GB",
  "country": "GB",
  "kind": "show",
  "id": "wire-7",
  "seasons": [1, 2],
  "order": "aired"
}
```

```json
{
  "title": "The Wire",
  "sort_title": "Wire",
  "original_title": "The Wire",
  "overview": "Baltimore, from both sides of the law.",
  "tagline": "Listen carefully.",
  "certificate": "15",
  "release_date": "2002-06-02",
  "year": 2002,
  "genres": ["Crime", "Drama"],
  "studios": ["HBO"],
  "ids": {"imdb": "tt0306414", "tmdb": "1438", "plugin:films": "wire-7"},
  "artwork": [
    {"kind": "poster", "url": "https://img.example/wire.jpg", "language": "en", "width": 1000, "height": 1500},
    {"kind": "backdrop", "url": "https://img.example/wire-wide.jpg"}
  ],
  "videos": [
    {"kind": "trailer", "site": "YouTube", "key": "9qK-VGjMr8g", "name": "Trailer", "language": "en",
     "published": "2008-01-01T00:00:00Z"}
  ],
  "credits": [
    {"name": "Dominic West", "ids": {"tmdb": "17178", "plugin:films": "west"},
     "photo": "https://img.example/west.jpg", "kind": "actor", "role": "Jimmy McNulty"},
    {"name": "David Simon", "ids": {"tmdb": "5714"}, "kind": "creator"}
  ],
  "seasons": [
    {
      "number": 1,
      "title": "Season 1",
      "overview": "The Barksdale organisation.",
      "artwork": [{"kind": "poster", "url": "https://img.example/wire-s1.jpg"}],
      "episodes": [
        {"number": 1, "title": "The Target", "release_date": "2002-06-02",
         "artwork": [{"kind": "thumb", "url": "https://img.example/wire-1x01.jpg"}]}
      ]
    }
  ]
}
```

Every field is optional. A season and an episode are described by the same fields as a title,
besides `number`; of them the server keeps the fields and pictures, and an episode's credits.

- `artwork` kinds: `poster`, `backdrop`, `logo`, `thumb`, `banner`, best first.
- `videos` are links to videos hosted elsewhere. Kinds: `trailer`, `teaser`, `featurette`,
  `behind_the_scenes`, `deleted_scene`, `interview`, `scene`, `short`, `clip`, `blooper`,
  `theme_video`, `other`. A library keeps only the kinds it is set to.
- `credits` kinds: `actor`, `guest_star`, `director`, `writer`, `producer`, `composer`, `creator`.
  The server knows a person by any of their `ids`, your own included: two credits sharing an id,
  from any source, are one person, who gains the ids each brings. A credit with no ids is not kept.
- `ids` are kept on the title, so the next match is handed them; give your own as `plugin:{id}`.

### `POST /search/v1/search`

List titles by a name, for an admin choosing a match by hand. The admin's choice is pinned as the
plugin's own id, and the title matched again with it.

```json
{"settings": {"api_key": "…"}, "kind": "movie", "title": "Jaws", "year": 1975}
```

```json
{
  "results": [
    {"id": "jaws-1", "title": "Jaws", "original_title": "Jaws", "year": 1975,
     "poster": "https://img.example/jaws.jpg"}
  ]
}
```

### `POST /rate/v1/ratings`

Say what sites' readers and critics make of a title, found by its ids. Scores are out of 100,
whatever scale the site uses (IMDb's 8.1 is 81). Sites: `imdb`, `tmdb`, `rotten_tomatoes`,
`rotten_tomatoes_audience`; any other is dropped.

```json
{"settings": {"api_key": "…"}, "kind": "movie", "ids": {"imdb": "tt0073195", "tmdb": "578"}}
```

```json
{"ratings": [{"site": "imdb", "score": 81, "votes": 640000}, {"site": "rotten_tomatoes", "score": 97}]}
```

### `POST /person/v1/person`

Say what is known of someone a title credits, found by their ids. Answer `404` for someone the
plugin does not know.

```json
{"settings": {"api_key": "…"}, "ids": {"tmdb": "17178", "plugin:films": "west"}}
```

```json
{
  "name": "Dominic West",
  "biography": "An English actor.",
  "born": "1969-10-15",
  "birthplace": "Sheffield, England",
  "photo": "https://img.example/west.jpg"
}
```

`died` is a date like `born`, where there is one.

### `POST /list/v1/list`

Answer a list's films or shows, in its order: the titles a collection holds, or a remote library
plays. A list is named as the plugin names its lists; an admin gives that name as the list's id.
Answer `404` for a list the plugin does not have. A title of a kind the manifest does not name, or
with no ids, is left out.

```json
{"settings": {"api_key": "…"}, "id": "best-of-1995"}
```

```json
{"titles": [{"kind": "movie", "ids": {"imdb": "tt0113277"}, "title": "Heat", "year": 1995}]}
```

### `POST /stream/v1/streams`

Answer the copies the plugin streams of a film, by its ids, or of an episode, by its show's ids and
its `season` and `episode`, best first; a remote library plays them. `url` is where the bytes are
now, `http` or `https`, which may be the plugin's own host though it is not public; it may change
each time, and is never kept. `key` is which bytes they are, the same each time they are offered,
as a file's name and size: a stream with no key, or no web address, is left out.

```json
{"settings": {"api_key": "…"}, "kind": "show", "ids": {"imdb": "tt0306414"}, "season": 1, "episode": 1}
```

```json
{
  "streams": [
    {"key": "file:The.Wire.S01E01.mkv:1503238553", "url": "http://films-plugin.local:9000/play/abc",
     "name": "The Wire S01E01 1080p", "filename": "The.Wire.S01E01.mkv", "size": 1503238553}
  ]
}
```

### `POST /subtitles/v1/search`

Answer the subtitles the plugin has for a film, by its ids, or an episode, by its show's ids and
its numbers, in `language`, an IETF tag such as `en` or `pt-BR`. `hash` is the file's
[OpenSubtitles hash](https://trac.opensubtitles.org/projects/opensubtitles/wiki/HashSourceCodes),
where the copy is one file; mark a subtitle made for that very file `for_release`. The server lists
those first, then the most downloaded, beside other providers' for a viewer to pick from, and
fetches the first that a library's settings want for titles that have none.

```json
{"settings": {"api_key": "…"}, "kind": "movie", "ids": {"imdb": "tt0113277"}, "hash": "8e245d9679d31e12", "language": "en"}
```

```json
{
  "subtitles": [
    {"id": "7", "language": "en", "release": "Heat.1995.1080p.BluRay", "hearing_impaired": false,
     "forced": false, "for_release": true, "downloads": 1200}
  ]
}
```

### `POST /subtitles/v1/fetch`

Answer a subtitle `search` found, by its `id`, as SubRip.

```json
{"settings": {"api_key": "…"}, "id": "7"}
```

```json
{"subrip": "1\n00:00:01,000 --> 00:00:04,000\nWhat are you, a cop?\n"}
```

## Calling the server

A plugin that acts on the server, rather than only answering it (one that scans a folder a
download landed in, or sets an episode's markers), calls its API as any client does, with an API
key: declare the server's address and a key as settings, the key `secret`, and the admin makes one
under **Settings → API keys** and sets both.

```json
"settings": [
  {"key": "photon_url", "name": "photon-server address", "required": true},
  {"key": "photon_key", "name": "photon-server API key", "secret": true, "required": true}
]
```

```sh
# scan the folder of a library a download landed in
curl -X POST "$PHOTON_URL/api/v1/admin/libraries/$LIBRARY_ID/scan?path=/srv/films/Heat%20(1995)" \
  -H "Authorization: Bearer $PHOTON_KEY"
```

A key acts as the admin who made it, as Jellyfin's and Emby's do, and stands until it is revoked
there; revoke it when the plugin is removed. The API is described at `GET /api/v1/openapi.json`.

A plugin is a service that runs on its own, so it keeps its own schedule: the server runs no task
of a plugin's, as Jellyfin does for its plugins, which have no process of their own.

Nor does a plugin check passwords, as Jellyfin's LDAP plugin does: a directory signs the household
in through OpenID Connect, by Authentik, Keycloak or Authelia in front of it, and a Jellyfin app
whose profile has no password signs in by pairing.

## Versions

The manifest's `protocol` is the version of the manifest and of what every call shares (settings,
locale, ids, errors); the server registers only plugins that speak one it does. Each capability has
a version of its own: a change to one capability's calls is a new version of it, and changes
nothing for a plugin that does not answer it. A new capability is no new version of anything.

Within a version the server may add optional fields to what it sends, so a plugin ignores fields
it does not know, and a plugin may leave out any optional field. Anything a plugin could notice
otherwise (a field renamed, removed or newly required, a changed meaning) is a new version.

## Stremio addons

A [Stremio addon](https://github.com/Stremio/stremio-addon-sdk/tree/master/docs) is a plugin too,
registered by the address of its `manifest.json`, as Stremio installs one:

```sh
curl -X POST https://photon.example/api/v1/admin/plugins \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"url": "https://aiostreams.example/stremio/…/manifest.json", "protocol": "stremio"}'
```

Its id is its manifest's, made lower case letters, digits and hyphens
(`com.stremio.torrentio.addon` is `plugin:com-stremio-torrentio-addon`), unless `id` names it: two
installs of one addon, configured apart, need ids of their own. An addon's configuration is in its
address, a debrid service's key among it, so the address is kept as a secret is: nothing shows it
past its host, and no error names it.

Each of its catalogs of films (`movie`) or shows (`series`) is a list, named as `{type}/{id}` as the
manifest lists it: `movie/top`. A list collection can hold a catalog's titles, as it does an
MDBList list's. A catalog is read a page at a time, by `skip`, to 500 titles.
