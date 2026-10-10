# Installing photon-server

photon-server runs beside PostgreSQL 18 and Valkey 9. Docker Compose runs all three from one
file; a native install runs the server from a package or an archive and uses a PostgreSQL and
Valkey you install.

## Docker

The image is `ghcr.io/olivertgwalton/photon-server`, for amd64 and arm64. Make a folder for the
server and save this in it as `compose.yml`:

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

Save this beside it as `.env`, with two passwords of your own, letters and digits only as they go
into URLs, and the folder holding your libraries:

```sh
POSTGRES_PASSWORD=
VALKEY_PASSWORD=
MEDIA_DIR=/srv/media
```

Then start it:

```sh
docker compose up -d
```

Open `http://<server>:8640` from the server's local network to set it up, as the
[README](../README.md#deploy) describes.

To update, pull the new image and recreate the server:

```sh
docker compose pull && docker compose up -d
```

### Hardware transcoding

For an Intel or AMD GPU, pass its devices to the server, under `server:`:

```yaml
    devices: [/dev/dri:/dev/dri]
```

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

The server's log says which it encodes on as it starts, and Settings › Server shows it.

### Several servers

Each machine running a server sets `PHOTON_HOSTNAME` in its `.env` to a name of its own, which
Settings › Server shows; it is `photon` where unset. See [cluster.md](cluster.md).

## Native

Each [release](https://github.com/olivertgwalton/photon-server/releases) has a macOS app in a DMG,
which carries its own PostgreSQL and Valkey, and an archive for Linux, macOS and Windows. Each
carries the server, its web app and the ffmpeg, ffprobe and yt-dlp it runs. An archive needs:

- **PostgreSQL 18**, with a database and a user of its own:
  ```sh
  createuser --pwprompt photon
  createdb --owner photon photon
  ```
  and, to back the database up and restore it, the PostgreSQL client tools, `pg_dump`,
  `pg_restore` and `psql`, on the server's `PATH`, no older than the PostgreSQL they reach.
- **Valkey 9**, where a distribution's own is older, from [valkey.io](https://valkey.io/download/).

### macOS

On a Mac with Apple silicon, open the release's DMG and drag Photon to Applications. It carries its
own PostgreSQL and Valkey, so there is nothing else to install or set.

Photon runs in the menu bar. Opened the first time, it makes its database in
`~/Library/Application Support/Photon`; each time, it starts PostgreSQL, Valkey and the server,
reached only through sockets there, and migrates the database. Open Photon opens it, Start at
Login starts it with the Mac, and Show Logs shows `~/Library/Logs/Photon`. Quitting it stops all
three at once. It encodes on VideoToolbox.

### Archives

Unpack the archive anywhere and keep its layout: the server finds its web app and tools from where
it is. Set the two addresses, migrate, and start it:

```sh
export PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon
export PHOTON_VALKEY_URL=valkey://localhost:6379
photon-server/bin/photon-server migrate
photon-server/bin/photon-server
```

On macOS the archive uses a PostgreSQL and Valkey you install, such as Homebrew's `postgresql@18`
and `valkey`. Its binaries are not signed, as the DMG's are: clear the download's quarantine before
the first run with `xattr -dr com.apple.quarantine photon-server`.

On Windows, run Photon in Docker Desktop with the compose file above: Valkey has no Windows build.
The archive is for those who run PostgreSQL and Valkey themselves, such as in WSL. Set the two
addresses as environment variables and run `bin\photon-server.exe`.
PostgreSQL's installer does not put its tools on `PATH`, and without them the server neither backs
up nor restores; add its `bin` folder, such as `C:\Program Files\PostgreSQL\18\bin`. Valkey has no Windows build, so run it in WSL or Docker.
The server encodes on an NVIDIA, Intel or AMD GPU, or in software.

To run it as a Windows service, from an administrator's PowerShell, with the archive unpacked in
`C:\photon-server` and the database migrated as above:

```powershell
sc.exe create photon-server binPath= "C:\photon-server\bin\photon-server.exe" start= auto
reg add HKLM\SYSTEM\CurrentControlSet\Services\photon-server /v Environment /t REG_MULTI_SZ /f `
  /d "PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon\0PHOTON_VALKEY_URL=valkey://localhost:6379"
sc.exe failure photon-server reset= 0 actions= restart/5000
sc.exe failureflag photon-server 1
sc.exe start photon-server
```

It runs as Local System, which must be able to read the libraries, and keeps its cache, its dumps
and its log, `photon-server.log`, in `C:\Windows\System32\config\systemprofile\AppData\Local\photon-server`.
Stopping it plays its streams to their end first; the failure actions start it again after a
restore. The PostgreSQL tools must be on the system's `PATH` for the service to find them.

A server stopped for a restore exits with status 75 to be started again: the Mac app and the
Windows service above do, and anything else running the server must too.
