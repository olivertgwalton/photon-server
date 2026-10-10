# Installing photon-server

photon-server runs beside PostgreSQL and Valkey. Docker Compose runs all three from one file.

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
