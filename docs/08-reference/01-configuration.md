---
title: "Configuration"
summary: "The environment, subcommands, ports, folders and tools photon-server reads, and what it keeps where."
---

photon-server reads almost nothing from its environment. Two addresses tell it where its database
and Valkey are; everything else (libraries, network, sign-in, transcoding, storage) is set in the
web app under Settings › Server and kept in the database, so every node of a server reads the same
settings and an admin changes them without restarting anything.

This page lists what the binary itself reads. For the variables of the Compose file, see the
[Docker Compose reference](02-docker-compose.md).

## Environment

| Variable | What it does |
|---|---|
| `PHOTON_DATABASE_URL` | The PostgreSQL database, as a URL: `postgres://photon:<password>@db/photon`. Required for everything but `openapi`. |
| `PHOTON_VALKEY_URL` | Valkey, as a `valkey://` URL, or `valkeys://` for TLS, with its user and password in it: `valkey://photon:<password>@valkey:6379`. Required to serve and to restore; `migrate` does without it. |

A command that needs one that is not set stops at once, saying "PHOTON_DATABASE_URL is not set".

**PostgreSQL** must be 18 or newer; an older one is refused as the server starts ("postgres … is
older than 18"). The URL takes PostgreSQL's own parameters, such as `sslmode`. Two the server
otherwise sets for itself:

- `pool_max_conns`: how many connections each node keeps, by default its CPUs plus 48, so a long
  scan never queues the requests behind it.
- `application_name`: what each node is called where PostgreSQL lists who is connected, by default
  `photon-server` and the machine's name.

**Valkey** may be shared with other servers: every key a server writes is under
`photon:<server id>:`, so give its user only those keys (`~photon:*`), as the Compose file does.
Valkey holds nothing durable: sessions, playbacks, the jobs' coordination and what the nodes tell
each other. Redis answers too.

The binary takes no flags. The platform's own environment decides the folders below
(`XDG_CACHE_HOME`, `XDG_CONFIG_HOME` and `HOME` on Linux) and where tools are found (`PATH`).

## Subcommands

| Command | What it does |
|---|---|
| `photon-server` | Serve, on port 8640, until it is told to stop. |
| `photon-server migrate` | Bring the database's schema up to this binary's, then exit. Run it before serving a new version: a server whose database is at another version refuses to start ("database is at …, this binary needs … (run photon-server migrate)"). It may run beside the old server, as no migration holds a large table. |
| `photon-server restore <dump>` | Replace the database with a dump the server made, migrate it to this binary's schema, and clear the server's keys in Valkey. It refuses, changing nothing, a dump made by a newer version, and a database anything else is connected to, naming who: stop every node first. See [Back up and restore](../07-maintain/01-backups.md). |
| `photon-server openapi` | Write the API's OpenAPI description to standard output, with no database. |

Anything else is refused with `usage: photon-server [migrate | restore <dump> | openapi]`.

### Stopping, and exit codes

On `SIGTERM` or an interrupt, a serving node drains: it takes no new streams, `/health` answers
`503` so a balancer sends new clients elsewhere, and it plays the streams it is serving to their
end, two hours at most, before it stops. Told to stop a second time, it stops at once. The log says
"draining: playing streams to their end; stop again to stop at once".

| Exit code | |
|---|---|
| `0` | It stopped as asked. |
| `1` | It stopped on an error, which the last log line names ("photon-server stopped"). |
| `75` | It stopped for a restore an admin started from the web app, and is to be started again: whatever runs it must restart it on this code, as Docker's `restart: unless-stopped` and the Windows service's failure actions do. |

### As a Windows service

Run by Windows' service manager, the server notices and runs as a service named `photon-server`.
A stop drains its streams as `SIGTERM` does, and a shutdown of the machine stops it at once. Having
no console, it writes its log to `photon-server.log` in its cache folder; that file is appended to
and never rotated. See [Install natively](../01-get-started/03-install-natively.md).

## Ports

| Port | What answers |
|---|---|
| 8640/tcp | The API, the web app, and `/health`. HTTP and HTTPS are served on this one port: a connection whose first byte begins a TLS handshake is answered over TLS, with the certificate set under Settings › Server › Network, and anything else as plain HTTP, as that page's **Secure connections** says. The port is fixed; a container maps it to any port the host likes. |
| 8640/udp | Discovery: apps on the local network broadcast "who is PhotonServer?" and are told the server's address. Only while Settings › Server › Network › **Discovery** is On. A port the server cannot have is a warning, "clients must be given the server's address", and nothing else. |
| 8096/tcp | Jellyfin's API, for Jellyfin's own apps, only while Settings › Server › Network › **Jellyfin apps** is On; it is Off by default, and the **Port** there changes it. It is bound as 8640 is, and serves HTTPS beside HTTP in the same way. See [Use Jellyfin apps](../04-access/05-jellyfin-apps.md). |

A transcode's or remux's HLS is served on 8640 like everything else. Beyond these, the server
listens only on loopback ports of its own, chosen as it runs, for ffmpeg to read from and write to;
nothing outside the machine reaches them.

## Folders

The server keeps two folders of its own, in the user's cache folder and config folder:

| Folder | Linux | macOS | Windows |
|---|---|---|---|
| Cache | `$XDG_CACHE_HOME/photon-server`, else `~/.cache/photon-server` | `~/Library/Caches/photon-server` | `%LocalAppData%\photon-server` |
| Backups | `$XDG_CONFIG_HOME/photon-server/backups`, else `~/.config/photon-server/backups` | `~/Library/Application Support/photon-server/backups` | `%LocalAppData%\photon-server\backups` |

On Windows both are in the local folder, as the config folder roams with the user's profile and
dumps are too big to. The image sets `XDG_CACHE_HOME=/var/cache` and `XDG_CONFIG_HOME=/var/lib`, so
a container keeps them at `/var/cache/photon-server` and `/var/lib/photon-server/backups`.

What the cache folder holds:

| Inside the cache | |
|---|---|
| `node` | This node's id. Keep it: a node that loses it joins the server as a new node, and the old one is listed as not running. |
| `artwork`, `previews` | Pictures and timeline previews, where Settings › Server › Storage keeps them on disk rather than in a bucket. See [Storage](../07-maintain/05-storage.md). |
| `hls` | Remuxes and transcodes being played. A transcode is refused while less than 2 GiB is free here. |
| `subtitles`, `fetched-subtitles` | Subtitles read out of files, and those fetched from providers. |
| `downloads` | Downloads converted for a device. |

## An install's layout

A release archive and the image lay the server out as an install does:

```
bin/photon-server
bin/ffmpeg, bin/ffprobe, bin/yt-dlp        (in the archives)
share/photon-server/web                    the web app
share/photon-server/jellyfin-web           Jellyfin's web app, for its API
```

The server finds its web apps in `../share/photon-server` from the binary, so
`/usr/local/bin/photon-server` reads `/usr/local/share/photon-server/web`. Without one, it serves
the API alone. It finds each tool beside the binary first, then on `PATH`.

## Tools

| Tool | |
|---|---|
| `ffmpeg`, `ffprobe` | Required. A release no older than 8; a snapshot build, which names no version, is refused ("not a release build"). The image and the archives carry jellyfin-ffmpeg. |
| `yt-dlp` | Optional. Without it there is no theme music from ThemerrDB. |
| `pg_dump`, `pg_restore`, `psql` | Optional, no older than the PostgreSQL they reach. Without them the server neither backs up nor restores, and logs "the database is neither backed up nor restored". |

As it starts, the server logs what it found (`"msg":"media tools"`), and warns of what an ffmpeg
build lacks: "intros and credits are found from chapters only: ffmpeg has no chromaprint muxer", and
"styled subtitles play only on clients that draw them: ffmpeg has no libass". Settings › Server ›
Dashboard shows the same.

## Logs

The server logs to standard error, one JSON object a line, with `time`, `level`, `msg` and the
event's own fields. A statement taking longer than 200 ms is logged as a "slow query", without its
values. See [Health, logs and metrics](../07-maintain/03-health-and-metrics.md), and
[Fix playback and connection problems](05-troubleshooting.md) for what the messages mean.
