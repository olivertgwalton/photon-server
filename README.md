# photon-server

A media server for films and television, written in Go.

## Develop

Needs PostgreSQL 18, Valkey, and FFmpeg 9 or newer (`ffmpeg` and `ffprobe` on the `PATH`, or
`PHOTON_FFMPEG` and `PHOTON_FFPROBE`). The server's own FFmpeg, for Linux on x86-64 and arm64, is
`build/ffmpeg/build.sh`: it cross-compiles with Zig from macOS or Linux (see `build/ffmpeg/SOURCE.md`).

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

Libraries are scanned every 12 hours, and as their files change: each folder is watched (inotify on
Linux) and a library is scanned a minute after its last change. Network shares send no change
events, so a library on one is scanned on the schedule; `photon-server library set -name NAME
-monitor off` stops watching a library. A large library may need a higher
`fs.inotify.max_user_watches`; the server says so when it runs out.

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
