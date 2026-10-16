---
title: "Configuration"
summary: "The settings photon-server reads from outside the web app: two addresses, its commands, ports, folders and tools."
---

Almost everything about photon-server is set in the web app, under Settings › Server, and kept in its database. You change it there without restarting anything. Outside the web app, the server reads only two addresses: where its database is, and where Valkey is (a small store for what is happening right now, such as films playing).

If you installed with Docker, the Compose file sets these for you. See the [Docker Compose reference](02-docker-compose.md).

## The two addresses

Set these as environment variables before the server starts.

| Variable | What it does |
|---|---|
| `PHOTON_DATABASE_URL` | The PostgreSQL database, such as `postgres://photon:<password>@db/photon`. Required. |
| `PHOTON_VALKEY_URL` | Valkey, such as `valkey://photon:<password>@valkey:6379`. Use `valkeys://` for an encrypted connection. Required to run the server and to restore; not needed to migrate. |

If one is missing, the server stops at once and says so, such as "PHOTON_DATABASE_URL is not set".

PostgreSQL must be version 18 or newer. An older one is refused as the server starts.

## Commands

| Command | What it does |
|---|---|
| `photon-server` | Runs the server, on port 8640, until it is told to stop. |
| `photon-server migrate` | Updates the database for this version, then exits. Run it before starting a new version; see [Update photon-server](../07-maintain/02-updating.md). |
| `photon-server restore <file>` | Replaces the database with a backup, then exits. Stop every server first. See [Back up and restore](../07-maintain/01-backups.md). |
| `photon-server openapi` | Prints a description of the server's API, for developers. Needs no database. |

The server takes no other options.

### Stopping

When you stop the server, it first lets the films it is sending play to the end, for up to two hours, and takes no new ones. Stop it a second time to stop it at once.

When it exits, it reports why:

| Exit code | Meaning |
|---|---|
| `0` | It stopped as asked. |
| `1` | It stopped because of an error. The last line of the log says what. |
| `75` | It stopped for a restore started in the web app, and should be started again. Docker's `restart: unless-stopped` and the Windows service do this. |

## Ports

| Port | What it is for |
|---|---|
| 8640 (TCP) | The web app, the apps and the API. Both secure (HTTPS) and plain (HTTP) connections use this one port; Settings › Server › Network › **Secure connections** says which are allowed. In Docker, you can map it to any port on the host. |
| 8640 (UDP) | Lets apps on your home network find the server by themselves, while Settings › Server › Network › **Discovery** is On. This doesn't work for a server run from the standard Compose file; see [Install with Docker](../01-get-started/02-install-docker.md#publish-other-ports). |
| 8096 (TCP) | For Jellyfin's own apps, only while Settings › Server › Network › **Jellyfin apps** is On. Off by default. The **Port** there changes it. See [Use Jellyfin apps](../04-access/05-jellyfin-apps.md). |

## Folders

The server keeps two folders of its own.

| Folder | Docker | Linux | Mac | Windows |
|---|---|---|---|---|
| Cache | `/var/cache/photon-server` | `~/.cache/photon-server` | `~/Library/Caches/photon-server` | `%LocalAppData%\photon-server` |
| Backups | `/var/lib/photon-server/backups` | `~/.config/photon-server/backups` | `~/Library/Application Support/photon-server/backups` | `%LocalAppData%\photon-server\backups` |

On Linux, `XDG_CACHE_HOME` and `XDG_CONFIG_HOME` move them, if set.

The cache folder holds pictures and previews (unless you use a [storage bucket](../07-maintain/05-storage.md)), the video being converted right now, converted downloads, subtitles, and the server's own identity.

> **Important:** Keep the cache folder between updates. A server that loses it joins as a new one, and pictures you uploaded are only there.

## Tools

| Tool | Needed for |
|---|---|
| FFmpeg and ffprobe | Required. Version 8 or newer, from a release. The Docker image and the download archives include them. |
| yt-dlp | Optional. Without it, there is no theme music from ThemerrDB (an online collection of theme tunes). |
| `pg_dump`, `pg_restore`, `psql` | Optional, for backups and restores. Must be no older than your PostgreSQL. |

The server looks for each tool beside itself first, then on the system path. Its log lists what it found as it starts, under `media tools`, and the dashboard shows the same.

## Logs

The server writes its log to its standard error output. See [Health, logs and metrics](../07-maintain/03-health-and-metrics.md#the-log) for where to read it, and [troubleshooting](05-troubleshooting.md) for what the messages mean.

## Good to know

- The database address accepts PostgreSQL's own options, such as `sslmode`, and two the server otherwise sets itself: `pool_max_conns` (how many connections each server keeps open) and `application_name`.
- Several photon-server installs can share one Valkey; each keeps to its own keys.
- On Windows, run as a service, the server writes its log to `photon-server.log` in its cache folder. The file grows and is never trimmed. See [Install natively](../01-get-started/03-install-natively.md).
- A release archive puts the web app in `share/photon-server` beside `bin`. Without it, the server runs the API alone.
