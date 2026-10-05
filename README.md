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

Titles are described by their file names, then any Kodi NFO beside them, and films and shows are
matched on TMDB in `PHOTON_METADATA_LANGUAGE` (default `en-US`, whose region picks certificates).
The server ships its own TMDB token and TheTVDB key; set `PHOTON_TMDB_TOKEN`, or
`PHOTON_TVDB_KEY` (with `PHOTON_TVDB_PIN` for a subscriber key), to use yours. Each library takes
metadata from `nfo` and `tmdb` by default, most trusted first; change that with
`photon-server library set -name NAME -sources nfo,tvdb,tmdb`. TheTVDB describes shows only. What a reader edits
or an NFO says is never replaced by a match.

Metadata providers are plugins: `GET /api/v1/admin/providers` lists each with what it can do (describe
titles, rate them) and what it needs set. TMDB gives its own score; MDBList gives IMDb's, Rotten
Tomatoes' critics and audience, Metacritic's, Letterboxd's and Trakt's once an admin sets its free
key (`PATCH /api/v1/admin/providers/mdblist` with `{"settings": {"api_key": "…"}}`) and a library
takes it (`-sources nfo,tmdb,mdblist`). Ratings are scored out of 100.

Libraries are scanned every 12 hours, and as their files change: each folder is watched (inotify on
Linux) and a library is scanned a minute after its last change. Network shares send no change
events, so a library on one is scanned on the schedule; `photon-server library set -name NAME
-monitor off` stops watching a library. A large library may need a higher
`fs.inotify.max_user_watches`; the server says so when it runs out.

A title a client cannot play as it is has its video copied into HLS where it can, with its audio
encoded, or its video encoded to H.264, HDR tone mapped to SDR. Encoding is in software unless
`PHOTON_HWACCEL` names a device: `videotoolbox`, `vaapi` or `qsv` (on `PHOTON_HWACCEL_DEVICE`,
default `/dev/dri/renderD128`) or `nvenc` (on CUDA device `PHOTON_HWACCEL_DEVICE`, default `0`). The
server encodes a test picture on it at start, and falls back to software if it will not.

Run several nodes against one Postgres and Valkey behind a load balancer and each says where its
peers reach it in `PHOTON_NODE_ADDRESS` (`http://10.0.0.5:8640`): a request for a stream's segments
that lands on another node is handed to the node making them.

The database is dumped every three days with `pg_dump` (`PHOTON_PG_DUMP`, no older than the
Postgres it dumps) into `PHOTON_BACKUP_DIR` (by default the user config folder's `photon-server/backups`), keeping
the newest three. Put one back into an empty database with
`pg_restore --no-owner -d postgres://… photon-….dump`.

The first admin is made on the command line, before anyone can sign in:

```sh
go run ./cmd/photon-server profile add -name Oliver -role admin
```
`GET /readyz` answers 204 while Postgres and Valkey are reachable and 503 otherwise.
Integration tests create and drop a database per test on the server `TEST_DATABASE_URL` names.

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Metadata

This product uses the TMDB API but is not endorsed or certified by TMDB.

Metadata provided by [TheTVDB](https://thetvdb.com). Please consider adding missing information or
subscribing.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
