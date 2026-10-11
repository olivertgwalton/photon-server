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

The host lets its CPU idle between requests, and every server pays for it: at 10 requests a second each answers 2–6× slower than back to back. The paced table is what a client waits for on an idle host. The back-to-back table is the work each server does.

## Latency

Bold marks the fastest server on each row.

### Back to back

p50 / p95 in milliseconds.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **0.18** / 0.28 | 0.37 / 1.03 | 18.1 / 22.6 | 0.24 / 0.53 |
| Recently added films, 20 | **1.9** / 3.3 | 3.1 / 4.3 | 11.5 / 13.6 | 10.8 / 11.6 |
| Continue watching, 20 | **1.4** / 2.5 | 4.6 / 7.3 | 16.1 / 20.2 | 1.6 / 1.8 |
| Films A–Z, 60 | **4.8** / 6.2 | 8.7 / 12.6 | 20.7 / 25.0 | 10.2 / 11.7 |
| Shows A–Z, 60 | 8.7 / 11.5 | 36.8 / 41.4 | 26.3 / 28.2 | **4.6** / 4.9 |
| Search "star" | **2.2** / 4.1 | 4.0 / 6.0 | 73.3 / 79.3 | 8.1 / 8.3 |
| Search "incep" | **1.5** / 2.4 | 5.2 / 6.1 | 10.7 / 11.6 | 4.2 / 4.4 |
| Film page | **2.3** / 3.7 | 3.1 / 4.8 | 11.2 / 12.5 | 59.7 / 63.5 |
| Show page | **2.3** / 3.2 | 6.4 / 9.9 | 23.0 / 24.6 | 4.0 / 5.5 |
| Seasons | in the show page | **4.2** / 5.1 | 8.1 / 9.1 | 8.6 / 9.6 |
| Season 1's episodes | **1.6** / 2.7 | 1.7 / 2.1 | 17.8 / 20.9 | 2.4 / 3.7 |
| Similar titles | **3.6** / 5.2 | 0.3 / 0.5 ¹ | 44.1 / 48.7 | 51.8 / 55.0 |

### Paced, 10 requests a second

p50 / p95 in milliseconds.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **2.1** / 3.9 | 4.5 / 5.5 | 33.3 / 39.4 | 2.2 / 2.7 |
| Recently added films, 20 | **10.3** / 14.0 | 15.6 / 18.4 | 30.2 / 36.6 | 31.6 / 36.1 |
| Continue watching, 20 | **9.2** / 14.2 | 23.4 / 30.5 | 33.2 / 37.1 | 10.4 / 11.6 |
| Films A–Z, 60 | **20.3** / 22.9 | 30.3 / 36.3 | 33.9 / 40.3 | 30.7 / 36.4 |
| Shows A–Z, 60 | 22.1 / 28.1 | 40.3 / 44.1 | 34.5 / 37.5 | **15.4** / 17.9 |
| Search "star" | **9.6** / 12.9 | 17.4 / 21.9 | 72.5 / 77.1 | 25.0 / 30.7 |
| Search "incep" | **7.0** / 26.3 | 19.1 / 22.9 | 29.3 / 33.4 | 16.4 / 22.1 |
| Film page | **9.4** / 27.4 | 19.6 / 25.4 | 31.6 / 37.8 | 62.4 / 65.6 |
| Show page | **12.3** / 15.9 | 25.3 / 28.1 | 34.1 / 37.4 | 19.9 / 22.2 |
| Seasons | in the show page | **22.3** / 26.0 | 31.5 / 34.2 | 30.6 / 35.6 |
| Season 1's episodes | **10.6** / 14.8 | 12.7 / 14.9 | 33.8 / 37.9 | 13.8 / 15.7 |
| Similar titles | 23.9 / 29.9 | 4.0 / 4.8 ¹ | 42.6 / 45.4 | 53.7 / 55.2 |

¹ Silo's similar titles came back empty for every title tried. Its recommendations had not been built on this server, so its time isn't comparable.

Notes on the rows:
- **Silo's home** also comes whole from one call. That call took 35.4 ms back to back and 39.9 ms paced.
- **Jellyfin** finds no film or show titled "star" and still takes 72 ms to say so.

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

`ab -k -c 8 -n 800`, in requests per second, with the p50 in milliseconds in brackets. Silo's per-key rate limit was turned off for this run.

| Endpoint | photon | silo | Jellyfin | Plex |
|---|---|---|---|---|
| Libraries | **18,884** (0) | 6,211 (1) | 186 (43) | 7,442 (1) |
| Recently added films | **1,712** (4) | 1,007 (7) | 264 (30) | 87 (92) |
| Continue watching | **2,098** (3) | 608 (13) | 115 (69) | 559 (14) |
| Films A–Z, 60 | **645** (12) | 416 (18) | 122 (64) | 83 (97) |
| Shows A–Z, 60 | **327** (24) | 109 (71) | 100 (80) | 135 (60) |
| Search "star" | **1,237** (6) | 776 (10) | 58 (137) | 88 (91) |
| Search "incep" | **2,668** (3) | 669 (11) | 229 (35) | 133 (60) |
| Film page | **1,238** (5) | 927 (8) | 119 (67) | 26 (312) |
| Show page | **1,068** (7) | 631 (12) | 47 (173) | 178 (45) |
| Seasons | in the show page | **793** (10) | 315 (25) | 118 (67) |
| Season 1's episodes | 1,638 (5) | **1,768** (4) | 168 (47) | 242 (33) |
| Similar titles | **849** (9) | 6,673 (1) ¹ | 79 (100) | 20 (396) |
| Home, whole | — | 112 (69) | — | — |

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

- **Shows A–Z.** Back to back, photon takes 8.7 ms against Plex's 4.6 ms. A card reads every picture a title has and ranks them in Go, and a show has about twice a film's pictures: 51 against 26.5. Ranking in SQL was slower on the current artwork index.
- **Season 1's episodes.** Silo and photon are level here: 1.7 ms against 1.6 ms back to back. Silo serves 8% more under load, 1,768 requests a second against 1,638.
- **First paint.** Silo paints its home first. Photon's web client starts with 116 requests where silo's starts with 12. That costs little on a LAN, more on a slow link.
