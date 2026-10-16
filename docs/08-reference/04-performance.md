---
title: "Photon beside Jellyfin, Plex and silo"
summary: "How fast photon-server answers, and what memory and CPU it takes, measured beside Jellyfin, Plex and silo on one machine."
---

We ran photon-server, Jellyfin, Plex and silo side by side on one Mac mini, with the same library of 358 films and about 85 shows. We timed how long each takes to answer the requests its own app sends for everyday pages (Home, a library, a film, a show, search), how many it can answer a second, and how much memory and processor each uses. Playback was not measured. photon-server answered fastest on almost every page and handled the most requests a second. Its own process uses little memory, but with its database it uses more in total than Jellyfin or Plex, which keep everything in one process.

## Setup

Measured on 11 October 2026 on one Mac mini: an M4 with 16 GB of memory, running Docker in OrbStack. The four servers read the same library: 358 films and 84–88 shows, depending on how each one groups them. Playback is left out, because it depends on the network and the debrid source more than on the server.

| Server | Version | Database |
|---|---|---|
| photon | v0.3.0-13 (390e3d4) | PostgreSQL 18, Valkey |
| silo | f7a3d65 (5 October 2026) | PostgreSQL 18 (pgvector), Redis |
| Jellyfin | 12.1.0 | SQLite |
| Plex | 1.43.4.10903 | SQLite |

## How it was measured

- **Where the client runs.** Every API request was sent from a container on the servers' Docker networks. OrbStack's port forward from the host adds a 200 ms tail to a few percent of requests, and this avoids it.
- **Plex and TLS.** Plex refuses plaintext from anything but loopback, so its numbers include TLS. The other three were measured over plain HTTP.
- **Equivalent requests.** Each endpoint is the request that server's own web client sends for the same view. Responses were gzip-compressed wherever the server offers it.
- **Paced latency.** 100 requests per endpoint at 10 a second, on one kept-alive connection, after 10 warm-up requests.
- **Back-to-back latency.** The same requests sent one after another with no gap.
- **Throughput.** `ab -k -c 8 -n 800` against each endpoint, three runs; the median is shown.
- **Memory and CPU.** Sampled with `docker stats` every two seconds for the whole run.

The host lets its CPU idle between requests, and every server pays for it: at 10 requests a second each answers 2–6× slower than back to back. The paced table is what a client waits for on an idle host. The back-to-back table is the work each server does.

## Latency

Bold marks the fastest server on each row.

### Back to back

p50 / p95 in milliseconds.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **0.12** / 0.17 | 0.35 / 1.40 | 15.7 / 16.8 | 0.22 / 0.39 |
| Recently added films, 20 | **0.95** / 1.6 | 2.8 / 3.9 | 10.7 / 11.9 | 11.0 / 11.9 |
| Continue watching, 20 | **0.79** / 1.7 | 4.7 / 7.2 | 16.4 / 17.7 | 1.6 / 1.8 |
| Films A–Z, 60 | **2.9** / 3.9 | 8.8 / 12.8 | 21.7 / 26.1 | 10.5 / 11.3 |
| Shows A–Z, 60 | **3.6** / 5.3 | 36.8 / 42.3 | 27.2 / 28.8 | 4.7 / 5.2 |
| Search "star" | **2.0** / 2.7 | 3.9 / 5.0 | 74.6 / 77.7 | 8.2 / 9.8 |
| Search "incep" | **1.3** / 1.6 | 5.0 / 6.0 | 11.2 / 12.1 | 4.3 / 4.7 |
| Film page | **1.3** / 1.9 | 2.9 / 3.8 | 11.3 / 12.8 | 60.9 / 62.3 |
| Show page | **1.6** / 1.9 | 4.8 / 7.4 | 23.5 / 25.1 | 4.2 / 4.9 |
| Seasons | in the show page | **4.0** / 5.0 | 8.3 / 9.4 | 8.5 / 9.0 |
| Season 1's episodes | **1.6** / 1.9 | 1.7 / 2.2 | 19.3 / 22.3 | 2.4 / 2.5 |
| Similar titles | **3.0** / 4.8 | 0.3 / 0.6 ¹ | 45.0 / 48.5 | 51.8 / 54.3 |

### Paced, 10 requests a second

p50 / p95 in milliseconds.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **2.1** / 4.6 | 4.6 / 8.3 | 28.6 / 35.9 | 2.1 / 2.6 |
| Recently added films, 20 | **8.3** / 12.2 | 15.6 / 19.0 | 32.2 / 39.0 | 31.8 / 35.6 |
| Continue watching, 20 | **9.2** / 11.7 | 23.2 / 27.5 | 31.7 / 38.9 | 10.5 / 11.4 |
| Films A–Z, 60 | **14.7** / 17.5 | 30.1 / 34.1 | 33.3 / 39.1 | 32.3 / 37.3 |
| Shows A–Z, 60 | **16.9** / 23.5 | 40.5 / 43.5 | 33.6 / 37.4 | 22.4 / 25.0 |
| Search "star" | **11.0** / 14.6 | 22.0 / 24.6 | 76.7 / 81.3 | 31.1 / 34.8 |
| Search "incep" | **9.4** / 13.1 | 24.0 / 27.3 | 32.6 / 38.0 | 19.8 / 22.8 |
| Film page | **11.1** / 15.0 | 19.4 / 26.0 | 32.0 / 37.3 | 61.4 / 64.5 |
| Show page | **11.5** / 16.8 | 25.0 / 30.5 | 33.7 / 37.3 | 19.8 / 22.9 |
| Seasons | in the show page | **22.2** / 27.0 | 31.4 / 36.0 | 30.7 / 35.2 |
| Season 1's episodes | **10.5** / 15.0 | 12.6 / 14.8 | 34.2 / 39.2 | 13.7 / 15.7 |
| Similar titles | 19.3 / 25.7 | 3.9 / 4.4 ¹ | 48.3 / 51.1 | 53.5 / 56.0 |

¹ Silo's similar titles came back empty for every title tried. Its recommendations had not been built on this server, so its time isn't comparable.

Notes on the rows:
- **Silo's home** comes whole from one call. That call took 35.7 ms back to back and 40.0 ms paced.
- **Jellyfin** finds no film or show titled "star" and still takes 75 ms to say so.

## Page data

These timings cover the API calls each web client makes to draw a page, sent in parallel the way the client sends them. Each figure is the time from the first request to the last response, p50 / p95 in milliseconds over 30 loads.

| Page | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Home | **41** / 46 | 77 / 83 | 105 / 113 | 60 / 64 |
| Film page | 30 / 35 | **27** / 39 | 101 / 106 | 159 / 165 |
| Show and season 1 | **27** / 31 | 50 / 55 | 108 / 113 | 83 / 86 |
| Films library | **31** / 35 | 51 / 56 | 83 / 86 | 49 / 51 |

## Throughput

`ab -k -c 8 -n 800`, in requests per second, with the p50 in milliseconds in brackets: the median of three runs. Silo's per-key rate limit was turned off for these runs.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **17,160** (0) | 6,043 (1) | 183 (43) | 7,315 (1) |
| Recently added films | **2,731** (3) | 1,158 (6) | 253 (31) | 87 (92) |
| Continue watching | **3,309** (2) | 645 (12) | 116 (68) | 550 (15) |
| Films A–Z, 60 | **1,138** (7) | 427 (18) | 117 (67) | 87 (92) |
| Shows A–Z, 60 | **880** (9) | 108 (72) | 98 (81) | 145 (55) |
| Search "star" | **1,651** (5) | 769 (10) | 58 (138) | 88 (90) |
| Search "incep" | **2,884** (3) | 665 (11) | 222 (36) | 130 (61) |
| Film page | **1,845** (4) | 977 (7) | 123 (65) | 26 (311) |
| Show page | **1,627** (5) | 637 (12) | 47 (170) | 185 (43) |
| Seasons | in the show page | **802** (9) | 309 (26) | 119 (67) |
| Season 1's episodes | 1,699 (4) | **1,817** (4) | 159 (49) | 246 (33) |
| Similar titles | **1,125** (7) | 6,451 (1) ¹ | 78 (101) | 20 (398) |
| Home, whole | — | 111 (70) | — | — |

## Memory and CPU

From `docker stats`, in MiB. For the PostgreSQL containers this includes the page cache. The CPU column is the highest sample during the throughput runs; sampling every two seconds misses short bursts.

| Container | Idle | Peak | After the run | Peak CPU |
|---|---|---|---|---|
| photon | 102 | 359 | **79** | 93% |
| photon's PostgreSQL | 210 | 647 | 646 | 273% |
| photon's Valkey | 6 | 13 | 8 | 2% |
| silo | 144 | 272 | 228 | 391% |
| silo's PostgreSQL | 346 | 830 | 822 | 702% |
| silo's Redis | 5 | 16 | 9 | 2% |
| Jellyfin | 202 | 381 | 339 | 754% |
| Plex | **72** | 116 | 83 | 461% |

- **Smallest servers.** Plex and photon have the smallest server processes at rest. Photon's grew to 359 MiB while answering 17,000 requests a second and returned to 79 MiB afterwards.
- **Whole stacks.** Plex and Jellyfin keep their whole stack in one process. Photon's comes to about 730 MiB and silo's to about 1.1 GB, most of it PostgreSQL.
