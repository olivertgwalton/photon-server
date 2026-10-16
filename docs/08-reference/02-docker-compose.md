---
title: "Docker Compose reference"
summary: "Every service, setting and variable of the Compose file photon-server is installed with, and what each is for."
---

This page explains the Compose file in [Install with Docker](../01-get-started/02-install-docker.md)
line by line: the variables it reads from `.env`, each service, and what to add for GPUs and
Jellyfin's apps. Change the file knowing what each part does, and you keep a server that updates,
backs up and restores as it should.

## Variables

The file reads these from `.env` beside it:

| Variable | What it does |
|---|---|
| `POSTGRES_PASSWORD` | Required. The password of PostgreSQL's `photon` user, set when the database is first made and put into the server's `PHOTON_DATABASE_URL`. |
| `VALKEY_PASSWORD` | Required. The password of Valkey's `photon` user, put into the server's `PHOTON_VALKEY_URL`. |
| `MEDIA_DIR` | Required. The folder on the host holding your libraries, seen in the container as `/media`. |
| `PHOTON_IMAGE` | The image to run, `ghcr.io/olivertgwalton/photon-server:main` by default. Set it to `…:latest` to follow releases, or to a version to stay on one. |
| `PHOTON_HOSTNAME` | The node's name among the server's nodes, `photon` by default. Give each machine of a server of several a name of its own. |

Compose refuses to start without the three required ones, saying which is missing. Use letters and
digits only in the two passwords: they are put into URLs, where other characters would need
escaping.

The server itself reads only `PHOTON_DATABASE_URL` and `PHOTON_VALKEY_URL`, which the file builds
from the passwords. See [Configuration](01-configuration.md).

## The server's settings

The file declares the image and the two addresses once, as `x-server`, and both `server` and
`migrate` take them, so the two always run the same image against the same database.

```yaml
x-server: &server
  image: ${PHOTON_IMAGE:-ghcr.io/olivertgwalton/photon-server:main}
  environment:
    PHOTON_DATABASE_URL: postgres://photon:${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}@db/photon
    PHOTON_VALKEY_URL: valkey://photon:${VALKEY_PASSWORD:?set VALKEY_PASSWORD}@valkey:6379
```

## server

| Key | What it does |
|---|---|
| `hostname` | The node's name, from `PHOTON_HOSTNAME`. The server names its node by the machine's name, and a container's own name changes every time it is recreated, which would list a new node each update. |
| `stop_grace_period: 2h` | How long Docker waits for it to stop. Told to stop, the server plays the streams it is serving to their end, two hours at most, so an update cuts no one's film off; Docker must wait that long, or it kills the server first. `docker compose stop` a second time stops it at once. |
| `ports: ["8640:8640"]` | The web app, the API and `/health`, over HTTP and HTTPS on one port. Change the host's side (`"80:8640"`) to publish it elsewhere; the server's own port is always 8640. |
| `/media:ro` | Your libraries, read-only. A library is added at its path in the container, such as `/media/Films`. A library that is allowed to delete its titles' files needs the mount without `:ro`. |
| `cache:/var/cache/photon-server` | The cache: pictures, previews, transcodes being played, converted downloads, and the node's id. Keep it as a volume, so the node keeps its id and its pictures across updates. |
| `backups:/var/lib/photon-server/backups` | The database dumps the server makes, and restores from. Point it at a host folder (`/srv/photon-backups:/var/lib/photon-server/backups`) to copy them off the machine. |
| `depends_on` | Start the server only once `migrate` has finished and Valkey answers. |
| `restart: unless-stopped` | Start it again whenever it exits. A server stopped for a restore exits with code 75 to be started again, which this does. |

The image starts as root only to join the groups owning the GPU devices passed to it, then runs as
its own user, 10001, with no capabilities. Given `user:` in the file, it runs as that user instead,
which then needs to read the media, write the two volumes and open the GPU devices.

## migrate

```yaml
  migrate:
    <<: *server
    command: [migrate]
```

The same image, run as `photon-server migrate`: it brings the database to the image's schema and
exits. The server refuses to start against a database at another version, so this runs first on
every `docker compose up`, and does nothing when there is nothing to do. It needs only the database,
so it waits for `db` alone.

## db

| Key | What it does |
|---|---|
| `image: postgres:18-alpine` | PostgreSQL 18. The server refuses anything older. |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | The `photon` user and database, made the first time the volume is empty. Changing the password in `.env` later does not change it in the database. |
| `db:/var/lib/postgresql` | The database. This is the server's state: libraries, profiles, watch history, settings. |
| `shm_size: 256mb` | Shared memory for PostgreSQL, above Docker's 64 MB default. |
| `healthcheck` | `pg_isready`, so `migrate` starts only once the database takes connections. |

The image's `pg_dump` is the PostgreSQL 18 client, and must be no older than this server: keep both
at 18, or move both up together.

## valkey

Valkey holds what is live (sessions, playbacks, the jobs' coordination, what the nodes tell each
other) and nothing that cannot be lost.

| Argument | What it does |
|---|---|
| `--appendonly no`, `--save "300 1"` | No log of every write; a snapshot every five minutes where anything changed, which keeps sessions across a routine restart. |
| `--maxmemory 256mb`, `--maxmemory-policy volatile-lru` | At most 256 MB, making room by dropping the least recently used keys that expire anyway. |
| `--user default off` | Turns off the user with no password. |
| `--user photon on >${VALKEY_PASSWORD} ~photon:* &* +@all -@dangerous +info` | The server's user: its password, only keys under `photon:`, every channel, every command but the dangerous ones, and `INFO`, which the dashboard reads Valkey's version with. |
| `valkey:/data` | Where the snapshot is kept. |
| `healthcheck` | A `ping` as the `photon` user, so the server starts only once Valkey answers. |

Every key the server writes is under `photon:` and its own id, so several servers can share one
Valkey, each with this user.

## Volumes

| Volume | Holds | Back it up |
|---|---|---|
| `db` | The database. | Through the server's own backups, which are consistent; not by copying the volume while it runs. See [Back up and restore](../07-maintain/01-backups.md). |
| `backups` | The server's dumps. | Yes: copy them off the machine. |
| `cache` | Pictures, previews and transcodes. | Optional. |
| `valkey` | A snapshot of what is live. | No. |

## Add to it

Under `server:`, as [Install with Docker](../01-get-started/02-install-docker.md) shows:

- **Jellyfin's apps**: publish the Jellyfin port once Settings › Server › Network › **Jellyfin apps**
  is On, `ports: ["8640:8640", "8096:8096"]`, matching the **Port** set there.
- **An Intel or AMD GPU**: `devices: [/dev/dri:/dev/dri]`.
- **An NVIDIA GPU**: a `deploy.resources.reservations.devices` entry for the `nvidia` driver, with
  the `graphics` capability among the others, which lets libplacebo reach NVIDIA's Vulkan driver.

Discovery's UDP broadcasts do not reach a container on Docker's own network, so publishing 8640
over UDP does not make apps on the network list the server: give them its address. See
[Connect Photon and other apps](../01-get-started/05-connect-apps.md).

For several machines serving one library, each runs the `server` service alone against the shared
`db` and `valkey`, with a `PHOTON_HOSTNAME` of its own. See
[Run several servers](../05-playback/04-several-servers.md).
