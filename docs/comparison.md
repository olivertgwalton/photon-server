# Photon beside Jellyfin, Plex and silo

## Setup

Measured on 11 October 2026 on one Mac mini: an M4 with 16 GB of memory, running Docker in OrbStack. The four servers read the same library: 358 films and 84–88 shows, depending on how each one groups them. Playback is left out, because it depends on the network and the debrid source more than on the server.

| Server | Version | Database |
|---|---|---|
| photon | v0.3.0-3 | PostgreSQL 18, Valkey |
| silo | f7a3d65 (5 October 2026) | PostgreSQL 18 (pgvector), Redis |
| Jellyfin | 12.1.0 | SQLite |
| Plex | 1.43.4.10903 | SQLite |

## How it was measured

- **Where the client runs.** Every API request was sent from a container on the servers' Docker networks. OrbStack's port forward from the host adds a 200 ms tail to a few percent of requests, and this avoids it.
- **Plex and TLS.** Plex refuses plaintext from anything but loopback, so its numbers include TLS. The other three were measured over plain HTTP.
- **Equivalent requests.** Each endpoint is the request that server's own web client sends for the same view. Responses were gzip-compressed wherever the server offers it.
- **Paced latency.** 100 requests per endpoint at 10 a second, on one kept-alive connection, after 10 warm-up requests.
- **Back-to-back latency.** The same requests sent one after another with no gap.
- **Throughput.** `ab -k -c 8 -n 800` against each endpoint.
- **Memory and CPU.** Sampled with `docker stats` every two seconds for the whole run.
- **Page loads.** Each server's own web client, signed in, in Chromium at 1440×900. Plex is left out of this one: its web client served from the server sent the browser to plex.tv to sign in on every load.

The host lets its CPU idle between requests, and every server pays for it. At 10 requests a second each server answers 4–6× slower than back to back:

| | Back to back | 10 a second |
|---|---|---|
| photon films A–Z, 60 titles | 4.6 ms | 20 ms |
| photon shows A–Z, 60 titles | 8.1 ms | 27 ms |
| photon film page | 1.3 ms | 8 ms |
| Plex shows A–Z, 60 titles | 3.2 ms | 18 ms |

The paced figures below are what a client waits for on an idle host. The back-to-back figures are the work each server does.

## Latency

Paced, 10 requests a second, p50 / p95 in milliseconds. Bold marks the fastest server on each row.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **2.1** / 3.9 | 4.5 / 5.5 | 33.3 / 39.4 | 2.2 / 2.7 |
| Recently added films, 20 | **10.3** / 14.0 | — | 30.2 / 36.6 | 31.6 / 36.1 |
| Continue watching, 20 | **9.2** / 14.2 | — | 33.2 / 37.1 | 10.4 / 11.6 |
| Films A–Z, 60 | **20.3** / 22.9 | 30.3 / 36.3 | 33.9 / 40.3 | 30.7 / 36.4 |
| Shows A–Z, 60 | 22.1 / 28.1 | 40.3 / 44.1 | 34.5 / 37.5 | **15.4** / 17.9 |
| Search "star" | **9.6** / 12.9 | 17.4 / 21.9 | 72.5 / 77.1 | 25.0 / 30.7 |
| Search "incep" | **7.0** / 26.3 | 19.1 / 22.9 | 29.3 / 33.4 | 16.4 / 22.1 |
| Film page | **9.4** / 27.4 | 19.6 / 25.4 | 31.6 / 37.8 | 62.4 / 65.6 |
| Show page | **12.3** / 15.9 | 25.3 / 28.1 | 34.1 / 37.4 | 19.9 / 22.2 |
| Season 1's episodes | **10.6** / 14.8 | 12.7 / 14.9 | 33.8 / 37.9 | 13.8 / 15.7 |
| Similar titles | **23.9** / 29.9 | — | 42.6 / 45.4 | 53.7 / 55.2 |

Notes on the rows:
- **Silo** has no separate recently-added or continue-watching rows; its home is one call, which took 39.5 ms. It also has no similar-titles endpoint.
- **Jellyfin** finds no film or show titled "star" and still takes 72 ms to say so.
- **Photon's p95** on search and the film page comes from the idle host: back to back, those calls take 3–9 ms.

## Page data

These timings cover the API calls each web client makes to draw a page, sent in parallel the way the client sends them. Each figure is the time from the first request to the last response, p50 / p95 in milliseconds over 30 loads.

| Page | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Home | **54** / 59 | 78 / 83 | 98 / 102 | 60 / 63 |
| Film page | 32 / 35 | **26** / 31 | 95 / 101 | 160 / 164 |
| Show and season 1 | **31** / 50 | 50 / 54 | 101 / 105 | 83 / 87 |
| Films library | **38** / 43 | 54 / 57 | 78 / 84 | 49 / 51 |

## Home page in a browser

Signed in, with a warm cache, the case of a returning user. The figures are the range over repeated loads.

| | photon | silo | Jellyfin |
|---|---|---|---|
| First Contentful Paint | 96–192 ms | **60–92 ms** | 96–116 ms |
| Largest Contentful Paint | **184–256 ms** | 232–368 ms | 472–588 ms |
| API calls, and when the last one ends | 15, 129–204 ms | 45, 203–340 ms | 10, 266–406 ms |
| Images, and when the last one ends | 64, 245–336 ms | 26, 201–385 ms | 33, 303–452 ms |

Each client draws its own home, so the pages differ in content. Photon showed three rows with 12 posters in view, silo two rows with 7, and Jellyfin three rows of wide cards.

What each client downloads before it can sign anyone in, from a cold cache:

| | Requests | Transferred | Load event |
|---|---|---|---|
| photon | 116 | 332 KB | 156 ms |
| silo | 12 | 350 KB | 115 ms |
| Jellyfin | 120 | 2.1 MB | 2.6 s |
| Plex | 4 to start | 5.8 MB, uncompressed | — |

## Throughput

`ab -k -c 8 -n 800`, in requests per second, with the p50 in milliseconds in brackets.

| Endpoint | photon | Jellyfin | Plex |
|---|---|---|---|
| Libraries | **18,724** (0) | 201 (39) | 6,969 (1) |
| Recently added films | **1,764** (4) | 276 (29) | 91 (88) |
| Continue watching | **1,946** (4) | 121 (66) | 560 (14) |
| Films A–Z, 60 | **687** (11) | 126 (62) | 88 (91) |
| Shows A–Z, 60 | **319** (24) | 101 (78) | 143 (56) |
| Search "incep" | **2,650** (3) | 244 (32) | 138 (58) |
| Film page | **1,355** (5) | 122 (67) | 28 (285) |
| Show page | **1,087** (7) | 46 (175) | 192 (42) |
| Season 1's episodes | **1,594** (5) | 188 (42) | 258 (31) |
| Similar titles | **877** (9) | 81 (97) | 22 (371) |

Silo is left out of this table. It limits an API key to about 20 requests a second, and 85–90% of its burst requests were refused with 429. Of the requests it did answer, the shows list ran at 108 requests a second and its home at 110.

## Memory and CPU

From `docker stats`, in MiB. For the PostgreSQL containers this includes the page cache.

| Container | Idle | Peak | After the run | Peak CPU |
|---|---|---|---|---|
| photon | **66** | 118 | 94 | 340% |
| photon's PostgreSQL | 361 | 646 | 640 | 120% |
| photon's Valkey | 5 | 12 | 10 | 2% |
| silo | 205 | 304 | 227 | 209% |
| silo's PostgreSQL | 487 | 799 | 764 | 720% |
| silo's Redis | 5 | 16 | 7 | 1% |
| Jellyfin | 317 | 406 | 340 | 748% |
| Plex | **50** | 123 | 66 | 476% |

- **Smallest servers.** Plex and photon have the smallest server processes.
- **Whole stacks.** Plex and Jellyfin keep their whole stack in one process. Photon's comes to about 750 MiB and silo's to about 1 GB, most of it PostgreSQL.
- **Work per unit of CPU.** Photon answered 5–50× the requests of Jellyfin and Plex at a lower CPU peak.

## Security

| Check | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Content-Security-Policy on the web client | yes, with `frame-ancestors 'none'` | yes, but no `frame-ancestors` | none | none |
| `X-Content-Type-Options: nosniff` | yes | yes | no | no |
| CORS for an unknown origin | refused | refused | `*` | the origin echoed back |
| Failed sign-ins (30 for a user that doesn't exist) | 429 after 10 | 429 after 10 | never limited | signs in through plex.tv |
| Token accepted in the query string | no | no | no | yes, `X-Plex-Token` |
| A page of a million titles | refused, 400 | refused, 422 | returns 13.7 MB | returns 16.9 MB |
| Answered without signing in | name, version, OpenAPI document | API digest, sign-in providers | name, version, LAN address | machine id, version |
| Forged token | 401 | 401 | 401 | 401 |
| Path traversal on static files | refused | refused | refused | refused |
| TLS | behind a reverse proxy | behind a reverse proxy | behind a reverse proxy | enforced |
| Container user | uid 10001 | root | root | root, the server as `plex` |

## Where photon is slower

- **Shows A–Z.** Back to back, photon takes 8.1 ms and Plex 3.2 ms. A card reads every picture a title has and ranks them in Go, and a show has about twice a film's pictures: 51 against 26.5. Ranking in SQL was slower on the current artwork index.
- **First paint.** Silo paints its home first. Photon's web client starts with 116 requests where silo's starts with 12. That costs little on a LAN, more on a slow link.
