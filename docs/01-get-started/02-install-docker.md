---
title: "Install with Docker"
summary: "Run photon-server and everything it needs from one file, on a Linux machine, a NAS, or a Windows PC."
---

Docker is a free tool that runs apps in their own sealed-off space, with everything they need
packed in. It is the best way to keep photon-server running all the time on a home server, a NAS
or a Windows PC. You write one file, type one command, and the server starts every time the
machine does.

By the end of this page, the server is running and ready to [set up](04-first-run.md). To run it
without Docker, see [Install natively](03-install-natively.md).

## Before you start

Install [Docker](https://docs.docker.com/get-docker/) on the machine. On Windows and macOS, that
means Docker Desktop.

You also need to know which folder holds your films and shows, such as `/srv/media`.

## Write the Compose file

A Compose file is a list of what Docker should run. This one runs photon-server, plus the
database and the small store it keeps its information in (see [Requirements](01-requirements.md)).

1. Make a folder for the server, such as `photon`.
2. Save the text below in it as `compose.yml`. Copy it exactly; you do not need to change anything
   in it.

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

The lines starting with `#` are notes and do nothing.

## Set the passwords and the media folder

In the same folder, save a file called `.env` with this in it:

```sh
POSTGRES_PASSWORD=
VALKEY_PASSWORD=
MEDIA_DIR=/srv/media
```

1. After `POSTGRES_PASSWORD=` and `VALKEY_PASSWORD=`, type two passwords of your own. Use letters
   and digits only. You never need to type them anywhere else: they protect the server's database.
2. After `MEDIA_DIR=`, type the folder that holds your films and shows.

> **Important:** Inside Docker, your media folder is called `/media`. So when you
> [add a library](../03-library/02-libraries.md), a folder at `/srv/media/Films` on your machine is
> `/media/Films`.

The server can only read your media. It never changes or deletes your files.

## Start it

Open a terminal in the folder and type:

```sh
docker compose up -d
```

This starts everything and leaves it running in the background. The first time, Docker downloads
what it needs and prepares the database, which can take a few minutes.

Then, on a device on the same home network, open the server's address followed by `:8640` in a
browser, such as `http://192.168.1.20:8640`. You will see the **Set up** page. Carry on with
[Set up a new server](04-first-run.md).

> **Important:** Set the server up from your home network, at this address. Setup will not open
> through a reverse proxy (a separate program that forwards web traffic to the server), so add one
> afterwards if you want one.

To see what the server is doing, type:

```sh
docker compose logs -f server
```

Press Ctrl+C to stop reading. The server keeps running.

## Publish other ports

The Compose file opens port 8640, which is all the web app and Photon need. Two things work
differently:

- **Jellyfin apps.** If you turn on Settings › Server › Network › **Jellyfin apps**, the server
  also listens on port 8096. In `compose.yml`, change the line `ports: ["8640:8640"]` to
  `ports: ["8640:8640", "8096:8096"]`, then run `docker compose up -d` again. See
  [Use Jellyfin apps](../04-access/05-jellyfin-apps.md).
- **Finding the server on the network.** Photon usually lists servers on your network by itself.
  Docker blocks this, so a server run from this file is not listed. Type its address in Photon
  instead, as [Connect Photon and other apps](05-connect-apps.md#add-the-server) describes.

## Hardware transcoding

When a device cannot play a file as it is, the server converts it as it plays (transcoding). A
graphics card does this much faster than the processor. Docker hides the graphics card from the
server until you pass it in.

For Intel or AMD graphics, add this line under `server:` in `compose.yml`, lined up with `ports:`:

```yaml
    devices: [/dev/dri:/dev/dri]
```

For an NVIDIA card, first install the
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
on the machine, then add this under `server:`:

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

Run `docker compose up -d` again. The server tests the card as it starts and uses it if it works.
Settings › Server › **Dashboard** shows which one it chose. See
[Transcoding and hardware encoders](../05-playback/01-transcoding.md).

## Update

To move to the newest version, type:

```sh
docker compose pull && docker compose up -d
```

The first command downloads the new version. The second restarts the server on it, and updates the
database first. See [Update photon-server](../07-maintain/02-updating.md) for what to check before
and after.

## Good to know

- **Stopping is gentle.** When you stop or update the server, anyone watching keeps watching until
  their film or episode ends, for up to two hours. Stop it a second time to stop it at once.
- **Releases instead of the newest build.** The Compose file runs the newest build of photon-server.
  To get only finished releases, add this line to `.env`:

  ```sh
  PHOTON_IMAGE=ghcr.io/olivertgwalton/photon-server:latest
  ```

- **Windows and macOS.** Docker Desktop runs the same file. On a Mac, Docker cannot reach Apple's
  video hardware, so it converts video on the processor. The
  [Mac app](03-install-natively.md#macos-the-mac-app) uses the hardware.
- **Several servers.** If you run photon-server on more than one machine, give each a name of its
  own with `PHOTON_HOSTNAME=` in its `.env`. See [Run several servers](../05-playback/04-several-servers.md).
- Every setting `.env` can hold is listed in the
  [Docker Compose reference](../08-reference/02-docker-compose.md).
