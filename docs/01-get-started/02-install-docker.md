---
title: "Install with Docker"
summary: "Run photon-server, PostgreSQL and Valkey together from one Compose file."
---

Docker Compose is the way to run photon-server from boot on a Linux machine, a NAS or a Windows
PC. One file runs the server beside the PostgreSQL 18 and Valkey 9 it keeps its state in, and you
end with a server answering on port 8640, ready to [set up](04-first-run.md). To run it without
Docker, see [Install natively](03-install-natively.md).

## The image

The image is `ghcr.io/olivertgwalton/photon-server`, for amd64 and arm64. It carries the server,
its web app, Jellyfin's web app for the [Jellyfin API](../04-access/05-jellyfin-apps.md),
jellyfin-ffmpeg, yt-dlp, the PostgreSQL 18 client tools it backs up with, the GPU drivers
jellyfin-ffmpeg loads, and the fonts styled subtitles are drawn in.

Its tags:

- `main`, the newest build of the main branch, which the Compose file below uses unless told
  otherwise.
- A release's version, without its `v`, and `latest`, the newest release.
- A commit's full hash, for each build of the main branch.

To follow releases instead of the main branch, set `PHOTON_IMAGE` in `.env`:

```sh
PHOTON_IMAGE=ghcr.io/olivertgwalton/photon-server:latest
```

## Write the Compose file

Make a folder for the server and save this in it as `compose.yml`:

```yaml compose.yml
name: photon

x-server: &server
  image: ${PHOTON_IMAGE:-ghcr.io/olivertgwalton/photon-server:main}
  environment:
    PHOTON_DATABASE_URL: postgres://photon:${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}@db/photon
    PHOTON_VALKEY_URL: valkey://photon:${VALKEY_PASSWORD:?set VALKEY_PASSWORD}@valkey:6379

services:
  server:
    <<: *server
    # Its name among the server's nodes, which a container's own would change on every recreate.
    hostname: ${PHOTON_HOSTNAME:-photon}
    # Stopping, it plays its streams to their end first, up to two hours; stop it again to stop at
    # once.
    stop_grace_period: 2h
    ports: ["8640:8640"]
    volumes:
      # Libraries are added at their path inside the container: /media/Films and the like.
      - ${MEDIA_DIR:?set MEDIA_DIR to the folder holding your libraries}:/media:ro
      - cache:/var/cache/photon-server
      - backups:/var/lib/photon-server/backups
    depends_on:
      migrate: { condition: service_completed_successfully }
      valkey: { condition: service_healthy }
    restart: unless-stopped

  migrate:
    <<: *server
    command: [migrate]
    depends_on:
      db: { condition: service_healthy }

  db:
    image: postgres:18-alpine
    environment:
      POSTGRES_USER: photon
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: photon
    volumes:
      - db:/var/lib/postgresql
    shm_size: 256mb
    healthcheck:
      test: [CMD, pg_isready, -U, photon, -d, photon]
      interval: 5s
      retries: 12
    restart: unless-stopped

  valkey:
    image: valkey/valkey:9-alpine
    # Nothing durable lives here: snapshots only keep live sessions across a routine restart.
    command:
      - valkey-server
      - --appendonly
      - "no"
      - --save
      - "300 1"
      - --maxmemory
      - 256mb
      - --maxmemory-policy
      - volatile-lru
      - --user
      - default
      - "off"
      - --user
      - photon
      - "on"
      - ">${VALKEY_PASSWORD}"
      - "~photon:*"
      - "&*"
      - +@all
      - -@dangerous
      # The server's own version, for the admin dashboard; INFO is in @dangerous.
      - +info
    volumes:
      - valkey:/data
    healthcheck:
      test: [CMD, valkey-cli, --user, photon, --pass, "${VALKEY_PASSWORD}", --no-auth-warning, ping]
      interval: 5s
      retries: 12
    restart: unless-stopped

volumes:
  db:
  valkey:
  cache:
  backups:
```

What it runs:

- **server**, photon-server itself, on port 8640. It reads your media read-only under `/media`,
  keeps its cache (pictures, previews, transcodes and converted downloads) in the `cache` volume and its
  database dumps in `backups`.
- **migrate**, which brings the database up to the image's version and exits. The server starts
  only once it has finished, so a new image never runs against an old schema.
- **db**, PostgreSQL 18.
- **valkey**, Valkey 9, holding what is live: sessions, playbacks, the jobs queue and what the
  server's nodes tell each other. Its user may touch only keys under `photon:`.

Stopping the server lets the streams it is serving play to their end, up to two hours, so nobody's
film is cut off by an update. Stop it a second time to stop it at once.

## Set the passwords and the media folder

Save this beside `compose.yml` as `.env`, with two passwords of your own and the folder holding
your libraries:

```sh
POSTGRES_PASSWORD=
VALKEY_PASSWORD=
MEDIA_DIR=/srv/media
```

Use letters and digits only in the two passwords: they go into the addresses the server reaches
PostgreSQL and Valkey at, where other characters would need escaping.

Everything under `MEDIA_DIR` appears in the container under `/media`. A library is added at its
path inside the container, so `/srv/media/Films` on the host is `/media/Films` when you
[add a library](../03-library/02-libraries.md).

## Start it

```sh
docker compose up -d
```

The first start pulls the images, makes the database and migrates it. Check it is up:

```sh
curl -i http://localhost:8640/health
```

`204 No Content` means the server reaches PostgreSQL and Valkey. Then open `http://<server>:8640`
from a device on the same network and [set the server up](04-first-run.md). Setup is open only to
a device on the server's local network reaching it directly, so do it before you put a reverse
proxy in front.

To read the server's log, which is JSON, one line an event:

```sh
docker compose logs -f server
```

## Publish other ports

The Compose file publishes 8640 over TCP, which is all the web app and Photon need. Two things
need more.

- **Jellyfin apps.** Turned on under Settings › Server › Network › Jellyfin apps, the server
  answers Jellyfin's API on a port of its own, 8096 by default. Publish it under `server:`, as
  `ports: ["8640:8640", "8096:8096"]`. See [Use Jellyfin apps](../04-access/05-jellyfin-apps.md).
- **Discovery.** Apps find a server on the network by broadcasting on UDP 8640. A broadcast does
  not reach a container on Docker's own network, so apps there do not list the server: give them
  its address instead, as [Connect Photon and other apps](05-connect-apps.md) describes.

## Hardware transcoding

For an Intel or AMD GPU, pass its devices to the server, under `server:`:

```yaml
    devices: [/dev/dri:/dev/dri]
```

The server starts as root only to join the groups that own those devices, then runs as its own
user, 10001, so no host's `render` group id needs looking up.

For an NVIDIA GPU, install the
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
on the host, then add under `server:`:

```yaml
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              # graphics is what lets libplacebo use NVIDIA's Vulkan driver; the toolkit
              # mounts only compute and utility by default.
              capabilities: [gpu, compute, utility, video, graphics]
```

As it starts, the server tries NVENC, then Quick Sync and VAAPI on each render device, and encodes
on the first that encodes a test picture, or in software where none does. Its log says which
(`"msg":"encoding video"`), and so does Settings › Server › Dashboard.
[Transcoding and hardware encoders](../05-playback/01-transcoding.md) covers choosing one yourself.

## Update

Pull the new image and recreate the server; `migrate` runs first on its own:

```sh
docker compose pull && docker compose up -d
```

See [Update photon-server](../07-maintain/02-updating.md) for what to check before and after.

## Several servers

Each machine running a server sets `PHOTON_HOSTNAME` in its `.env` to a name of its own, which
Settings › Server › Dashboard shows; it is `photon` where unset. See
[Run several servers](../05-playback/04-several-servers.md).

## Windows and macOS

Docker Desktop runs the same file on Windows and on a Mac. On a Mac it encodes in software, as a
container cannot reach VideoToolbox; the [Mac app](03-install-natively.md#macos-the-mac-app) encodes on it.

Every variable the Compose file reads is listed in the
[Docker Compose reference](../08-reference/02-docker-compose.md).
