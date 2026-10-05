# Metadata plugins

A metadata plugin is a web service that tells photon-server about films and shows. It can be
written in any language and run anywhere the server can reach over HTTP. An admin registers it by
its address; from then on it is a provider like the built-in TMDB, TheTVDB and MDBList: it is
listed among the providers, takes settings such as an API key, and a library can take metadata
from it, ranked where the admin puts it.

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

# a library that takes it, after any NFO beside the files and before TMDB
photon-server library set -name Films -sources nfo,plugin:films,tmdb
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
- **Ids** are maps from a provider to the title's or person's id there: `imdb`, `tmdb`, `tvdb`,
  and the plugin's own, `plugin:{id}`, once it has given one.
- **Kinds** are `movie` and `show`. A plugin is asked only about the kinds its manifest names.
- **Errors.** Answer a `4xx` or `5xx` status, with an RFC 9457 problem (`application/problem+json`,
  `{"title": "…", "detail": "…"}`) where there is something to say. A `5xx`, a timeout or no
  answer at all is a plugin that is down: the server passes over it to the library's next source,
  as it does a provider whose key is not set. A `4xx` from `/match` or `/describe` fails the
  title's job, which an admin sees and can retry; from `/ratings` or `/person` it is logged and
  the title or person goes without.
- **Limits.** Each call has 20 seconds and may answer at most 8 MiB.
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
  "capabilities": ["describe", "search", "rate", "person"],
  "settings": [
    {"key": "api_key", "name": "API key", "secret": true, "required": true},
    {"key": "region", "name": "Region"}
  ]
}
```

- `id` is the plugin's slug: lower case letters, digits and hyphens, starting with a letter or
  digit, at most 63 characters. It may not change between manifests.
- `capabilities` are what the plugin answers: `describe` is `/match` and `/describe` together,
  `search` is `/search`, `rate` is `/ratings`, `person` is `/person`. Leave out an endpoint's
  capability and it is never called.

### `POST /match` (describe)

Find the title the server has. Where `ids` carries the plugin's own id, given before or pinned by
an admin, answer it. Answer an empty `id` when there is no confident match: a wrong match is worse
than none.

```json
{
  "settings": {"api_key": "…"},
  "kind": "movie",
  "title": "Jaws",
  "year": 1975,
  "ids": {"imdb": "tt0073195"}
}
```

```json
{"id": "jaws-1"}
```

### `POST /describe` (describe)

Say what is known of the title matched. For a show, `seasons` are the seasons the server wants,
numbered in `order` (`aired`, `dvd` or `absolute`); describe those you have.

```json
{
  "settings": {"api_key": "…"},
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
    {"name": "Dominic West", "ids": {"tmdb": "17178"}, "photo": "https://img.example/west.jpg",
     "kind": "actor", "role": "Jimmy McNulty"},
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
  The server knows people by their TMDB id: a credit without a `tmdb` id is not kept.
- `ids` are kept on the title, so the next match is handed them; give your own as `plugin:{id}`.

### `POST /search` (search)

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

### `POST /ratings` (rate)

Say what sites' readers and critics make of a title, found by its ids. Scores are out of 100,
whatever scale the site uses (IMDb's 8.1 is 81). Sites: `imdb`, `tmdb`, `rotten_tomatoes`,
`rotten_tomatoes_audience`, `metacritic`, `letterboxd`, `trakt`.

```json
{"settings": {"api_key": "…"}, "kind": "movie", "ids": {"imdb": "tt0073195", "tmdb": "578"}}
```

```json
{"ratings": [{"site": "imdb", "score": 81, "votes": 640000}, {"site": "rotten_tomatoes", "score": 97}]}
```

### `POST /person` (person)

Say what is known of someone a title credits, found by their ids. Answer `404` for someone the
plugin does not know.

```json
{"settings": {"api_key": "…"}, "ids": {"tmdb": "17178"}}
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

## Versions

The manifest's `protocol` is the version a plugin speaks; the server registers only plugins that
speak one it does. Within a version the server may add optional fields to what it sends, so a
plugin ignores fields it does not know, and a plugin may leave out any optional field. Anything a
plugin could notice otherwise (a field renamed, removed or newly required, a changed meaning) is a
new version.
