---
title: "Docker Compose reference"
summary: "What each part of photon-server's Compose file does, and what to add to it."
---

This page explains the Compose file from [Install with Docker](../01-get-started/02-install-docker.md), part by part. Read it before you change the file, so updates, backups and restores keep working.

The file runs four things:

- **server**: photon-server itself.
- **migrate**: updates the database before the server starts, then stops.
- **db**: the PostgreSQL database, which holds everything the server knows.
- **valkey**: a small store for what is happening right now, such as films playing.

## The .env file

The Compose file reads these from `.env` in the same folder:

| Variable | What it does |
|---|---|
| `POSTGRES_PASSWORD` | Required. The database password, set when the database is first created. |
| `VALKEY_PASSWORD` | Required. Valkey's password. |
| `MEDIA_DIR` | Required. The folder on your machine that holds your films and shows. Inside the container it is `/media`. |
| `PHOTON_IMAGE` | Which version to run. `ghcr.io/olivertgwalton/photon-server:main` by default. Use `…:latest` to follow releases, or a version number to stay on one. |
| `PHOTON_HOSTNAME` | The server's name, `photon` by default. Only matters when you [run several servers](../05-playback/04-several-servers.md): give each machine its own. |

Docker refuses to start if a required one is missing, and says which.

> **Important:** Use only letters and digits in the two passwords. They go into addresses, where other characters cause problems.

> **Note:** Changing `POSTGRES_PASSWORD` later doesn't change the password of a database that already exists.

## server

| Setting | What it does |
|---|---|
| `ports: ["8640:8640"]` | Makes the web app reachable on port 8640. To use another port on your machine, change the first number, such as `"80:8640"`. |
| `/media:ro` | Your films and shows, read-only. Add libraries at paths inside it, such as `/media/Films`. A library allowed to delete files needs this without `:ro`. |
| `cache:/var/cache/photon-server` | The cache folder: pictures, previews, video being converted, and the server's identity. Keep it as a volume so it survives updates. |
| `backups:/var/lib/photon-server/backups` | Where backups are kept. Point it at a folder on your machine, such as `/srv/photon-backups:/var/lib/photon-server/backups`, to copy them elsewhere more easily. |
| `hostname` | Set from `PHOTON_HOSTNAME`, so the server keeps the same name each time it is updated. |
| `stop_grace_period: 2h` | When stopped, the server lets films play to the end, for up to two hours. This tells Docker to wait that long. |
| `restart: unless-stopped` | Starts the server again whenever it stops, including after a restore. |
| `depends_on` | Starts the server only after the database is updated and Valkey is ready. |

The server runs as its own user (10001), not as an administrator. If you add `user:` to run it as another user, that user must be able to read your media, write to the two volumes and use the graphics card.

## migrate

Runs `photon-server migrate` with the same version and database as the server, then stops. It runs every time you run `docker compose up`, and does nothing when the database is already up to date. The server won't start until it has finished.

## db

| Setting | What it does |
|---|---|
| `image: postgres:18-alpine` | PostgreSQL 18. The server refuses anything older. |
| `db:/var/lib/postgresql` | The database itself: your libraries, profiles, watch history and settings. |
| `shm_size: 256mb` | Gives PostgreSQL more working memory than Docker's default. |
| `healthcheck` | Makes `migrate` wait until the database is ready. |

> **Important:** The server's backup tools are version 18, and must be no older than the database. Keep both at 18, or move both up together.

## valkey

Valkey holds only what is happening right now, so nothing in it matters if it is lost. Its settings keep it small (at most 256 MB), save a snapshot every five minutes so a routine restart doesn't interrupt anything, and give the server its own password-protected user.

## Volumes

| Volume | Holds | Back it up |
|---|---|---|
| `db` | The database. | Use the server's own backups. Don't copy the volume while it runs. See [Back up and restore](../07-maintain/01-backups.md). |
| `backups` | The server's backups. | Yes: copy them to another machine. |
| `cache` | Pictures, previews and converted video. | Recommended, for uploaded pictures and avatars. |
| `valkey` | What is happening right now. | No. |

## What to add

Under `server:`, as [Install with Docker](../01-get-started/02-install-docker.md) shows:

- **Jellyfin's apps**: once Settings › Server › Network › **Jellyfin apps** is On, publish its port too: `ports: ["8640:8640", "8096:8096"]`, matching the **Port** set there.
- **An Intel or AMD graphics card**: `devices: [/dev/dri:/dev/dri]`.
- **An NVIDIA graphics card**: a `deploy.resources.reservations.devices` entry for the `nvidia` driver, including the `graphics` capability.

Apps on your network won't find a server run from this file by themselves; give them its address. See [Install with Docker](../01-get-started/02-install-docker.md#publish-other-ports).

To run several machines as one server, each runs only the `server` part against the shared `db` and `valkey`, with its own `PHOTON_HOSTNAME`. See [Run several servers](../05-playback/04-several-servers.md).
