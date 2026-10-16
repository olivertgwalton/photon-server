---
title: "Fix playback and connection problems"
summary: "What the server's refusals and log messages mean, and what to do about each."
---

When something does not work, photon-server nearly always says why: a client is answered a
problem with a `code` and a `detail`, and the server's log, one JSON line an event, says the rest.
This page goes through the messages you are most likely to meet, by what you were trying to do.

Read the log with `docker compose logs -f server`, or wherever your service manager keeps standard
error. Every message below is its `msg` field.

## The server does not start

| The log, or the command, says | What to do |
|---|---|
| `PHOTON_DATABASE_URL is not set` (or `PHOTON_VALKEY_URL`) | Set the variable. See [Configuration](01-configuration.md#environment). |
| `schema version mismatch: database is at …, this binary needs … (run photon-server migrate)` | Run `photon-server migrate` with this version, then start it again. The Compose file does this on every `up`. |
| `postgres … is older than 18` | Move to PostgreSQL 18 or newer. |
| `valkey: …` | The Valkey address, user or password is wrong, or Valkey is down. |
| `exec: "ffmpeg": executable file not found in $PATH`, `… -version: …`, `not a release build`, `version … is older than 8` | ffmpeg or ffprobe is missing, broken, a snapshot build, or too old. Put a release, 8 or newer, beside the binary or on `PATH`. |
| `waiting for a restore to finish before starting` | A restore is under way; the node starts once it ends. Meanwhile it answers `503` "the server is restoring its database". |
| `usage: photon-server …` | An argument it does not know: it takes `migrate`, `restore <dump>`, `openapi` or none. |

Once it serves, it logs `serving` with its address and version, and `encoding video` with what it
encodes on.

## The web app or an app cannot reach the server

Start with `/health`, from the machine and then from where the app is:

```sh
curl -i http://localhost:8640/health
```

- `204 No Content`: the node reaches PostgreSQL and Valkey and takes clients. The problem is the
  network between, a firewall, or the address the app was given.
- `503` with `not_ready`: the log says `not ready`, with `postgres: …` or `valkey: …`, naming which
  it cannot reach. A node that is stopping answers `503` too, on purpose, while its streams play
  out, and logs nothing for it.
- No answer at all: nothing is listening there. In Docker, check the port is published.

| The log says | What it means |
|---|---|
| `clients must be given the server's address` | The node could not take UDP 8640 for discovery, so apps on the network will not list it. Give them its address. In Docker this is expected: a broadcast does not reach a container on Docker's network. |
| `jellyfin's apps cannot reach this node` | Jellyfin apps are On under Settings › Server › Network, but the node could not take the port, usually as something else has it. It tries again every minute. In Docker, publish that port too. |
| `secure connections not applied` | The certificate or key set under Settings › Server › Network could not be read. The node keeps serving as it was, so a certificate caught mid-renewal is served until its files are whole again. |

HTTPS and HTTP share port 8640, and **Secure connections** says which a client may use. Reaching
the server from outside, behind a proxy or not, is in
[Reach your server from outside](../04-access/04-remote-access.md). A proxy's clients are told
apart only where **Trusted proxies** names the proxy; otherwise every client is the proxy, which
also decides who counts as on the local network.

## A title will not play

The player shows what the server answered. By its code:

| Code | Detail | What to do |
|---|---|---|
| `no_compatible_stream` (422) | The reasons, such as "Video codec" or "HDR" | Nothing the player plays can be made of this copy, even by transcoding. Try another player, or another copy. |
| `transcode_limit` (503) | "every node is already transcoding as many videos at once as it may: …" | Every transcode slot is taken. Wait, or raise **Transcodes at once** under Settings › Server for a node. |
| `transcode_limit` (503) | "every node is transcoding as many videos at once as it may, or is short of space to" | A node had less than 2 GiB free in its transcode cache. Its log says `transcode refused for want of space`, with the folder and the bytes free. Free space there. |
| `stream_limit` (503) | "every node is delivering as many streams at once as it may" | Each node is at its **Streams at once** or its **Egress** limit. |
| `not_found` (404) | "the playback has stopped, or lapsed" | The player went quiet for two minutes and the playback was ended. Start it again. |
| `provider_unavailable` (502) | "the provider is fetching the title; it plays once it has it" | A remote library's addon is still fetching the stream. Open it again shortly. See [Stremio addons](../06-integrations/03-stremio.md#how-a-title-plays). |
| `provider_unavailable` (502) | "nothing the provider offers of the title could be read" | Every stream the addon offered failed. |

The node limits are explained in [Run several servers](../05-playback/04-several-servers.md#what-each-node-does),
which applies to a server of one node too.

### It plays, but transcodes

Settings › Server › Dashboard shows each playback as `direct`, `remux` or `transcode`, and for a
transcode **Because of:** and the reasons:

| Reason | Why |
|---|---|
| Container | The player does not open the file's container. |
| Video codec, Video profile, Video level | The player does not play the video as it is. |
| Resolution, Bit depth | The picture is larger, or deeper, than the player plays. |
| HDR | The screen does not show the video's HDR. See [HDR and Dolby Vision](../05-playback/02-hdr.md). |
| Audio codec, Audio channels | The player does not play the audio as it is. |
| Bitrate | The file is above the quality chosen. A client outside the server's local networks is held to Settings › Server › Network › **Remote streams** › **Limit (Mbps)**. |
| Subtitles | The subtitles are pictures, drawn into the video. |
| Several files | The title is in several files, played as one. |
| Playback failed | The player failed to play it as it is, and asked again. |

Which networks are local is Settings › Server › Network › **Local networks**: this machine's and
the private ranges where none is set.

### Transcoding fails

The server tests its encoder as it starts and logs what it found:

| The log says | What it means |
|---|---|
| `encoding video` | The encoder it chose (`on`), its `device`, and whether it encodes HEVC. |
| `the encoder set encodes no test picture: encoding on the first that does` | The encoder an admin set for the node does not work; the node uses the first that does. Check the device is passed to the container. |
| `transcoding will fail: ffmpeg would not encode a test picture` | Not even software encoding works: the ffmpeg build is broken. |
| `encoding H.264 alone` | The encoder cannot encode HEVC; everything is transcoded to H.264. |
| `burning subtitles in on vaapi through the cpu instead` | The GPU will not draw subtitles in; the CPU does, which is slower. |
| `burning subtitles in will fail: ffmpeg would not burn one into a test picture` | Picture or styled subtitles cannot be drawn into a transcode. |
| `styled subtitles play only on clients that draw them: ffmpeg has no libass` | The build has no libass. |
| `remux failed` | One playback's ffmpeg failed; the line carries its error. |

Choosing an encoder is in [Transcoding and hardware encoders](../05-playback/01-transcoding.md).

## A library does not update

| The log says | What to do |
|---|---|
| `out of inotify watches: the rest of this library is scanned on schedule only; raise fs.inotify.max_user_watches` | The host's limit on watched folders is reached. Raise `fs.inotify.max_user_watches` on the host (a Docker container shares it). Until then the scheduled scan catches up. |
| `libraries are scanned on schedule only` | Watching for changes could not start at all on this node. |
| `folder not read`, `file left out` | One folder or file could not be read, with the path and why: usually permissions, or a mount that has gone. |
| `NFO not read` | An NFO beside a title could not be read; the title is described by the library's other sources. |

See [Keep libraries up to date](../03-library/06-keep-up-to-date.md).

## Integrations

| You see | Where to look |
|---|---|
| A plugin refused at registration (`400`) | The reason names what is wrong with its address or manifest. See [Install and manage plugins](../06-integrations/01-plugins.md#why-a-plugin-is-refused). |
| A tracker refuses to link (`409`) | "… is not set up: an admin sets its client id", or "… refused the client id". See [Sync with trackers](../06-integrations/04-trackers.md). |
| A webhook never arrives | Settings › Server › Jobs › **Given up** lists failed deliveries with the receiver's answer. See [Webhooks](../06-integrations/05-webhooks.md#when-a-delivery-fails). |
| An import fails | Its row on Settings › Server › Import history says why. |
| `the database is neither backed up nor restored` | `pg_dump`, `pg_restore` or `psql` is not on the server's `PATH`. See [Back up and restore](../07-maintain/01-backups.md). |

## Slow pages

A database statement taking over 200 ms is logged as `slow query`, with how long it took and the
statement, without its values. [Health, logs and metrics](../07-maintain/03-health-and-metrics.md)
shows how to watch the server over time.
