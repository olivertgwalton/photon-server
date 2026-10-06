# photon-server

A media server for films and television, written in Go.

## Run

The image (`ghcr.io/olivertgwalton/photon-server`, amd64 and arm64) carries jellyfin-ffmpeg's
portable build, for its hardware transcoding and tone mapping, with Intel's and AMD's VAAPI drivers,
Quick Sync's runtime and Intel's OpenCL on amd64, and Mesa's Vulkan. `deploy/` runs it with
PostgreSQL and Valkey:

```sh
cd deploy && cp .env.example .env    # set the passwords and MEDIA_DIR
docker compose up -d                 # add -f compose.intel.yml or -f compose.nvidia.yml for a GPU
docker compose exec server photon-server profile add -name Admin -role admin
docker compose exec server photon-server library add -name Films -kind movies /media/Films
```

## Develop

Needs PostgreSQL 18, Valkey, and FFmpeg 8 or newer (`ffmpeg` and `ffprobe` on the `PATH`, or
`PHOTON_FFMPEG` and `PHOTON_FFPROBE`); jellyfin-ffmpeg publishes portable macOS builds that match
the image's.

```sh
export PHOTON_DATABASE_URL=postgres://localhost/photon_dev
export PHOTON_VALKEY_URL=valkey://localhost:6379
go run ./cmd/photon-server migrate
go run ./cmd/photon-server
TEST_DATABASE_URL=postgres://localhost/postgres TEST_VALKEY_URL=valkey://localhost:6379 go test -tags integration ./...
```

The server listens on `:8640` (`PHOTON_LISTEN`) and names itself after the host (`PHOTON_NAME`).
Behind a reverse proxy, list the proxy's addresses in `PHOTON_TRUSTED_PROXIES` (for example
`172.16.0.0/12,127.0.0.1`); `X-Forwarded-For` is ignored from anyone else.

Clients on the local network find the server without being given its address: it listens on UDP at
the same port number as HTTP (`8640`, so open both protocols on that port), and answers a datagram
reading `who is PhotonServer?` (in any case) with its id, name, version and the address to reach it
on, as Jellyfin does on 7359. Only askers on this machine or a private or link-local network are
answered. In Docker a broadcast reaches the server only with host networking (`network_mode: host`),
as with Jellyfin. `PHOTON_DISCOVERY=off` stops it answering; if the port is taken the server says
so and serves HTTP regardless.

Titles are described by their file names, then any Kodi NFO beside them, and films and shows are
matched on TMDB in `PHOTON_METADATA_LANGUAGE` (default `en-US`, whose region picks certificates).
The server ships its own TMDB token and TheTVDB key; set `PHOTON_TMDB_TOKEN`, or
`PHOTON_TVDB_KEY` (with `PHOTON_TVDB_PIN` for a subscriber key), to use yours. Each library takes
metadata from `nfo` and `tmdb` by default, most trusted first; change that with
`photon-server library set -name NAME -sources nfo,tvdb,tmdb`. TheTVDB describes shows only. What a reader edits
or an NFO says is never replaced by a match.

A film or show is matched again every 30 days (a library's `refresh_days`, 0 never). An admin can ask now, as
with Jellyfin's and Plex's Refresh Metadata: `POST /api/v1/admin/titles/{id}/refresh` with
`{"mode": "missing"}` asks about the title and any season with an episode not yet described, ahead
of everything queued; `"all"` asks about every season and episode under it too. Either way each
provider's pictures are replaced with what it has now, and edits, locks and NFOs stand. Reading
its files again is the library scan's job.

A title's pictures are the files beside it first (`poster.jpg`, `fanart.jpg`…), then each
provider's best ten of a kind, in the library's order. An admin chooses another, as with Jellyfin's
Edit Images and Plex's poster chooser: `GET /api/v1/admin/titles/{id}/artwork/candidates?kind=poster`
lists what the providers had at the last match, each served like any picture at
`/api/v1/artwork/{id}`, and `PUT /api/v1/admin/titles/{id}/artwork/poster` with `{"id": "…"}`
makes one the title's own, above the files and through every refresh, until `DELETE` on the same
address gives it back.

Metadata providers are plugins: `GET /api/v1/admin/providers` lists each with what it can do (describe
titles, rate them) and what it needs set. TMDB gives its own score; MDBList gives IMDb's, Rotten
Tomatoes' critics and audience, Metacritic's, Letterboxd's and Trakt's once an admin sets its free
key (`PATCH /api/v1/admin/providers/mdblist` with `{"settings": {"api_key": "…"}}`) and a library
takes it (`-sources nfo,tmdb,mdblist`). Ratings are scored out of 100.

Collections are TMDB's box sets, shown once a library holds two of a set's titles, and an admin's
own (`POST /api/v1/admin/collections`). Each says which in its `origin` (`tmdb` or `user`). Only an
admin's has its titles set or is removed by hand; a TMDB set's titles follow TMDB, and it goes when
none are left. Either one's name and overview can be edited as any title's, and the edit stands.

Anyone can add a metadata provider, in any language, as a web service speaking the server's plugin
protocol ([docs/plugins.md](docs/plugins.md)). Register one by its address
(`POST /api/v1/admin/plugins` with `{"url": "http://films-plugin:9000"}`) and it is the provider
`plugin:ID`: listed with the others, set the same way, and taken by a library like any source
(`-sources nfo,plugin:films,tmdb`). A plugin that is down is passed over for the library's next
source. Removing one keeps what it said about titles until another source says otherwise.

An admin adding a library can browse the server's folders for it: `GET /api/v1/admin/folders`
answers the folders to start from (`/` and whichever of `/media`, `/mnt`, `/srv`, `/Volumes` and
the server's home are there, or each drive on Windows), and `?path=` an absolute folder's
subfolders, links to folders among them, up to a thousand and saying when there are more. Folders
whose names start with a dot are left out unless asked for (`&hidden=show`). It shows anything the
server's user can read, so only an admin may ask; in Docker that is the container's view, with your
media under `/media`.

Libraries are scanned every 12 hours, and as their files change: each folder is watched (inotify on
Linux) and a library is scanned a minute after its last change. Network shares send no change
events, so a library on one is scanned on the schedule; `photon-server library set -name NAME
-monitor off` stops watching a library. A large library may need a higher
`fs.inotify.max_user_watches`; the server says so when it runs out.

A network mount that stops answering blocks a read forever, so every FFmpeg run over a library
file has a limit: five minutes for one that reads part of a file (a probe, a chapter picture, an
intro's sound), five minutes plus the file at 8 MiB a second for one that reads all of it
(keyframes, trickplay), and five minutes without progress for a download's conversion. A run past
its limit is stopped and its job fails saying so, rather than holding its place for ever.

A title a client cannot play as it is has its video copied into HLS where it can, with its audio
encoded, or its video encoded to H.264, HDR tone mapped to SDR. Encoding is in software unless
`PHOTON_HWACCEL` names a device: `videotoolbox`, `vaapi` or `qsv` (on `PHOTON_HWACCEL_DEVICE`,
default `/dev/dri/renderD128`) or `nvenc` (on CUDA device `PHOTON_HWACCEL_DEVICE`, default `0`). The
server encodes a test picture on it at start, and falls back to software if it will not.

Each node encodes at most `PHOTON_MAX_TRANSCODES` videos at once (a number, or `unlimited`): by
default a quarter of its CPUs in software, at least one, and eight on a hardware encoder, the cap
NVIDIA puts on a GeForce card's sessions. A download's conversion takes one of those slots too, but
playback always wins: a play that needs a slot on a node at its limit stops a conversion there,
which goes back in the queue to start again from the beginning once a slot is free. Only when
plays hold every slot is a play that would encode video refused, with 503 `transcode_limit`, rather
than played worse; one played as it is or with its video copied is never refused. A transcode's slot
is freed as it stops, or a couple of minutes after its player goes quiet.
`GET /api/v1/admin/playbacks` says which node runs each playback, and how many videos the node
answering is transcoding against its limit (`transcodes.limit` is absent when unlimited), with
`transcodes.conversions` saying how many of them are conversions.

A text subtitle inside a file is sent beside an HLS playback as WebVTT. Reading one out means
reading the whole file, so the first time any of a file's text subtitles is asked for, all of them
are read out in that one pass, as Jellyfin does, and kept under `PHOTON_CACHE_DIR` in `subtitles`
until no one has played the file for a month. A player that gives up waiting does not stop the
pass, and finds the subtitles there when it asks again.

An admin's dashboard sees each playback as Jellyfin's does: `GET /api/v1/admin/playbacks` names the
profile, the device and app that started it and the address it played from, the title with its
pictures, the copy and its length, where it has got to, and how it plays: the reasons it could not
play as it is, each stream as it is in the file and what it is encoded to, whether a subtitle is
drawn into the picture, and the device encoding it. All of it is fixed as the playback starts, and
the event stream's snapshot and playback events carry each playback the same way. `DELETE
/api/v1/admin/playbacks/{id}` stops one: its remux ends on whichever node runs it, its player is
refused from then on, and the play is kept in the history where its player last said it was. A
title played as it is is read from a signed address that lasts a day, so its player can go on
reading the file it has; only its reports are refused.
Each copy says where its intro, credits, recap and preview are, so a player can offer to skip them.
A chapter named for one (Intro, Opening, End Credits, Previously…) marks it. Otherwise the server
compares the sound of a season's episodes, as Plex and Jellyfin's Intro Skipper do: the stretch two
episodes share near the start is the intro, near the end the credits. A season is compared ten
minutes after its episodes stop arriving, and any not yet compared at 3 a.m. That needs an FFmpeg
built with chromaprint, which the image's is; without it the server says so at start and reads
chapters only. An admin's own markers (`PUT /api/v1/admin/versions/{id}/markers`) outrank both,
and so does an admin's word that a part has none of a kind (`"absent": [{"kind": "intro", "part":
0}]`, parts counted from 0), which hides a wrong chapter or fingerprint match through rescans and
later comparisons. Each PUT replaces what was said of the copy; an empty one clears it.

Comparing sound reads the first ten minutes and the last stretch of every episode, which on a
library on a network share or a debrid mount of 4K remuxes is gigabytes an episode. Each library
chooses, as Plex's intro and credits detection and Jellyfin's per-library segment providers let it:
`photon-server library set -name NAME -markers chapters` (or `"markers": "chapters"` in
`PATCH /api/v1/admin/libraries/{id}`) offers only the markers chapters name, which costs no reads,
`-markers off` none, and `all`, the default, compares sound too. A library not comparing queues no
comparisons, and one already queued for it does nothing; fingerprints it found before are kept and
offered again if it goes back to `all`, whose next 3 a.m. run queues the seasons not yet compared.
An admin's own markers stand whatever the setting.

Each library makes previews of its videos ahead of time, as Plex and Jellyfin do: a picture of each
chapter, and trickplay sheets for scrubbing (a 320-pixel thumbnail every ten seconds, a hundred to
a JPEG sheet, HDR tone mapped). They are made from keyframes in the background, one part at a time
per node beside the other jobs, and kept under `PHOTON_CACHE_DIR` in `previews`; a two-hour film's
come to a few megabytes. `photon-server library set -name NAME -previews chapters` makes only the
chapter pictures, and `-previews off` none. Each night at two the server queues whatever is not yet
as its library asks and takes away what a library no longer wants. A file replaced by new bytes
loses its old previews at the scan that finds it; a file that is simply gone keeps them for 30 days,
so a share that is unmounted for a while does not come back to hours of remaking, and loses them
after that.

A client downloads a title for offline viewing at a most video bitrate, and width if it says, as
Plex's Downloads do. A copy already within both is downloaded as it is; any other is converted in
the background, on the same device as playback, into one MP4 of H.264 and AAC (HDR tone mapped to
SDR) under `PHOTON_CACHE_DIR`'s `downloads` folder, where the client fetches it, resuming as it
likes. Each node converts one title at a time, and only in a free transcode slot that a play may
take back (see `PHOTON_MAX_TRANSCODES`), so playback is not starved; a title asked for at the same
quality by several profiles is converted once. A converted file is deleted when the last
download needing it is removed, and downloads are forgotten a week after they are ready if no one
removes them; size the cache folder for the conversions waiting to be fetched.

Run several nodes against one Postgres and Valkey behind a load balancer and each says where its
peers reach it in `PHOTON_NODE_ADDRESS` (`http://10.0.0.5:8640`): a request for a stream's segments
that lands on another node is handed to the node making them.

The server keeps an activity log of what an admin reads later: sign-ins and refused ones (with the
device and address), plays started and stopped, libraries and profiles added and removed, scans
and the titles they found, failed tasks, backups, and jobs that failed for good. `GET
/api/v1/admin/activity` pages it, newest first (`kind` narrows it), and entries older than 30 days
are forgotten daily. `GET /api/v1/admin/events` is a Server-Sent Events stream for a dashboard:
first a `snapshot` of the tasks and jobs running, the scans going on and who is playing what on
every node, then each event as it happens, named by its kind (`task.finished`, `scan.progress`,
`playback.paused`…), with a comment every 15 seconds so proxies leave it open. It asks nginx not
to buffer it; another proxy may need buffering turned off for its path.

Clients keep their pages right without polling through `GET /api/v1/events`, the same kind of
stream for any signed-in profile, as Jellyfin's WebSocket and Plex's notifications do: a `hello`
with the scans going on, then `library.changed` (a library's titles `added`, `updated` and
`removed`, gathered for three seconds, so a scan of hundreds of files is a handful of events),
`title.updated` (matched, edited or given another picture), `scan.progress`, and
`userdata.changed` for the profile's own progress, marks, favourites and playlists from any
device. A profile is told only of libraries and titles it may see. Nothing is kept to resend, so
a client that reconnects asks again for what it shows, and one whose device switches profile
opens the stream again.

Webhooks are told of events as Plex's are: `POST /api/v1/admin/webhooks` with a `url` and the
`events` it wants (plays started, paused, resumed and stopped, sign-ins, profiles and libraries
added and removed, scans, titles added, failed tasks and backups) answers a `secret`, once. Each
event is POSTed as JSON (`event`, `at`, `server`, and the `profile`, `title` and `library` it is
about) with `X-Photon-Event` naming it and `X-Photon-Signature: sha256=` the hex HMAC-SHA256 of the
body under the secret. A receiver has 10 seconds to answer 2xx; anything else, a redirect too, is
tried again with the job queue's backoff, five times, and then shows among the dead jobs. `POST
/api/v1/admin/webhooks/{id}/test` sends it a `webhook.test`.

The database is dumped every three days with `pg_dump` (`PHOTON_PG_DUMP`, no older than the
Postgres it dumps) into `PHOTON_BACKUP_DIR` (by default the user config folder's `photon-server/backups`), keeping
the newest three. Put one back into an empty database with
`pg_restore --no-owner -d postgres://… photon-….dump`.

The first admin is made on the command line, before anyone can sign in:

```sh
go run ./cmd/photon-server profile add -name Oliver -role admin
```

A profile with a password changes it itself with `PUT /api/v1/me/password` (`current` and `new`, at
least 8 characters), which signs out every other device watching as that profile; wrong guesses are
limited as sign-ins are. A household profile, one with no password, is only ever chosen on a
signed-in device and cannot give itself one: an admin does, which lets it sign in by itself. It can
still set a PIN with `PUT /api/v1/me/pin`.

The API describes itself: `GET /api/v1/openapi.json` answers its OpenAPI 3.1 description, built
from the server's own routes as it starts, so it says what the running server takes and answers.
Point a client generator or a viewer such as Swagger UI at it; no token is needed.
`photon-server openapi` writes the same description to standard output with no database or
server running, which is what the web client generates its types from.

`GET /readyz` answers 204 while Postgres and Valkey are reachable and 503 otherwise.
`GET /api/v1/admin/server` shows an admin how the node answering was set up, for a dashboard: its
version and when it started, the OS, FFmpeg and FFprobe and whether they fingerprint sound, the
encoder and transcode limit, discovery, the listen address and trusted proxies, the cache and backup
folders with the space left on them (on Linux and macOS), the metadata language, whether Postgres
and Valkey answer and their versions, and the nodes that say where their peers reach them
(`PHOTON_NODE_ADDRESS`), each with when it last did. It is all set by the environment, so none of it
is changed here; no password or connection string is in it.
Integration tests create and drop a database per test on the server `TEST_DATABASE_URL` names.

## Web

`web/` is the server's web app, a SvelteKit app run by Bun: people log in, browse and play there,
and admins run the server from it. It is a thin server of its own in front of the API: it keeps
the session's token in an HTTP-only cookie and passes the browser's API calls on to
`PHOTON_API_URL` with the token added, so the browser talks to one origin and never sees a token.
Compose runs it as `web` on port 3000 (`ghcr.io/olivertgwalton/photon-server-web`), and the
server trusts its `X-Forwarded-For`, so sign-in limits and the activity log see each reader's own
address. It is reached over plain HTTP at its address (`http://192.168.1.10:3000`) or behind a reverse
proxy at HTTPS, where the session cookie is Secure; behind a proxy, give it
`ADDRESS_HEADER=x-forwarded-for` so it sees readers' addresses, and pass the `Host` header through (or set `HOST_HEADER=x-forwarded-host`).

To work on it, run the server as above and, from `web/`:

```sh
bun install
PHOTON_API_URL=http://localhost:8640 bun run dev   # http://localhost:5173
bun run check && bun run lint && bun test src && bun run test:e2e
bun run api   # after changing a route: regenerates src/lib/api/schema.d.ts
```

The API's types are generated from `photon-server openapi`; CI fails when
`web/src/lib/api/schema.d.ts` is older than the routes. The end-to-end tests run the built app
against a mock of the API (`web/e2e/mock-api.ts`), typed by the same schema.

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Metadata

This product uses the TMDB API but is not endorsed or certified by TMDB.

Metadata provided by [TheTVDB](https://thetvdb.com). Please consider adding missing information or
subscribing.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
