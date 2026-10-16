---
title: "Health, logs and metrics"
summary: "Check that photon-server is up, read what it logs, follow what it does on the dashboard, and scrape its metrics with Prometheus."
---

photon-server tells you how it is in four places: `/health`, for a load balancer or an uptime check; its log; the dashboard and Metrics pages in the web app; and `/metrics`, for Prometheus. This page covers each.

## Health

`GET /health` needs no sign-in. It answers `204 No Content` while the server reaches both PostgreSQL and Valkey and is taking new clients, and `503 Service Unavailable`, with the problem code `not_ready`, while it does not:

- PostgreSQL or Valkey is out of reach. Which one, and why, is in the log, not the answer, which may be seen by anyone.
- The server is stopping, playing its last streams to their end. A balancer that asks sends it no new clients, while the streams it has carry on.
- A restore is under way (see [back up and restore](01-backups.md)).

```sh
curl -i https://photon.example/health
```

Point a load balancer's or an uptime monitor's health check at it. A node draining answers `503` by design, so it is taken out of rotation without being thought broken.

## The log

The server writes its log to standard error, one JSON object a line, at the info level and above. Where to read it:

| Install | Log |
|---|---|
| Docker | `docker compose logs -f server` |
| Flatpak | the `logs` folder in `~/.var/app/io.github.olivertgwalton.Photon/data/photon` |
| Mac app | Show Logs, in Photon's menu: `~/Library/Logs/Photon` |
| Windows service | `photon-server.log`, in `C:\Windows\System32\config\systemprofile\AppData\Local\photon-server` |
| Archive | wherever you send its standard error |

Lines worth knowing, as the server starts:

- `media tools`, with the FFmpeg, ffprobe and yt-dlp it found and their versions.
- A warning that intros and credits are found from chapters only, where FFmpeg has no Chromaprint; and one that styled subtitles play only on clients that draw them, where it has no libass.
- `encoding video`, with the encoder chosen (see [transcoding](../05-playback/01-transcoding.md#what-the-server-encodes-on)).
- A warning that the database is neither backed up nor restored, where the PostgreSQL client tools are missing.
- `serving`, with its address and version, once it is up.

After that, each scheduled task logs `task started` and `task finished` with its result, each library scan `library scanned` with how many folders it read, and each job that fails `job failed` with its attempt and error.

## The dashboard

Settings › Server › Dashboard is the first page of the server's settings. From the top:

- **Needs attention**, where there is something to put right, each with a link: no libraries yet, jobs that failed every attempt, or a metadata provider a library asks that has not been set up.
- **Now playing**: each play, who is watching, on what, how far, and how it plays (Direct play, Direct stream or Transcode, with why and what each stream becomes). **Stop this play** ends it, keeping where it got to.
- **Running**: scans and the work queued behind them.
- **Server**: the version and how long it has run, the system, the FFmpeg, ffprobe and yt-dlp it runs, whether it has Chromaprint and libass, its encoder, role and transcodes, where it listens, whether it reaches PostgreSQL and Valkey and their versions, and its cache and backups folders with the space free on each. With several servers, its nodes follow.
- **Recent activity**, and **All activity** for Settings › Server › Activity, which keeps 30 days of plays, sign-ins, scans, failed tasks and the like.

The page updates itself as things happen; "reconnecting to live updates…" beside the version means it has lost the server's event stream for a moment.

## The Metrics page

Settings › Server › Metrics shows what every node is doing now, asked again every 5 seconds while the page is in view, with the last ten minutes drawn beside it:

- Across the nodes: streams by how they play, transcodes against the limit and how many were refused lately, bandwidth sent to players, nodes, and the films and episodes in the libraries with the space they hold.
- Each node: streams, bandwidth, CPU (as a percentage of one core), memory and transcodes.
- The job queue by kind and state, with how long the oldest job due has waited, and when each scheduled task last finished and how.

The page keeps no history; Prometheus does. It reads `GET /api/v1/admin/metrics`, which asks every node and lists one that does not answer within 2 seconds as not answering.

## Prometheus

Every server answers `GET /metrics` on its own port, 8640, in Prometheus' text format. It answers only clients on the server's local networks, as Settings › Server › Network sets them, or this machine's and the private ranges where none is set; anyone else is answered `404`. Behind a proxy, a client is known by the proxy's `X-Forwarded-For` only where it is among Settings › Server › Network's **Trusted proxies**.

```yaml
scrape_configs:
  - job_name: photon
    static_configs:
      - targets: ["photon.lan:8640"]
```

With several servers, list every node as a target.

| Metric | What it counts |
|---|---|
| `photon_build_info{version}` | The version the server runs. |
| `photon_node_info{node, role, state}` | Its name, role, and `active` or `draining`. |
| `photon_playbacks{method}` | Playbacks it serves now: `direct`, `remux` or `transcode`. |
| `photon_playback_starts_total{method}` | Playbacks started by clients asking it; one refused is not counted. |
| `photon_sent_bytes_total{delivery}` | Bytes of media sent to players, as a `file` or as HLS `segment`s. |
| `photon_transcodes{kind}` | Videos it encodes now, for a `playback` or a download's `conversion`. |
| `photon_transcode_slots` | The most it encodes at once; absent with no limit. |
| `photon_transcode_refusals_total{reason}` | Plays refused for want of an encoder: `full`, `no_space` or `no_encoder`. |
| `photon_hls_segment_wait_seconds` | How long a request for a segment waits for it to be made. |
| `photon_jobs_finished_total{kind, outcome}` | Jobs it ended, `done` or `failed`. |
| `photon_jobs{kind, state}` | Jobs queued, running, to run again (`rerun`) and `dead`. |
| `photon_jobs_oldest_due_seconds{kind}` | How long the queued job due now and waiting longest has been due. |
| `photon_task_last_finished_timestamp_seconds{task, result}` | When each task's last run ended, and how. |
| `photon_nodes{state}` | The nodes running, `active` or `draining`. |
| `photon_library_items{kind}` | Films (`movie`) and episodes in the libraries. |
| `photon_library_bytes{kind}` | The bytes those films and episodes hold on disk. |

The Go runtime's (`go_*`) and the process's (`process_*`) metrics come with them. The last six describe what the nodes share, so only the node running the scheduled tasks reports them; [several servers](../05-playback/04-several-servers.md#metrics) says how to query them across nodes without counting them twice.

Some alerts worth having:

- `photon_jobs{state="dead"} > 0`: work that failed every attempt, waiting on Settings › Server › Jobs.
- `time() - photon_task_last_finished_timestamp_seconds{task="backup_database", result="succeeded"} > 4 * 86400`, or that series absent, as it is when the last run failed: backups have stopped.
- `rate(photon_transcode_refusals_total[5m]) > 0`: people are being refused for want of transcode slots.
