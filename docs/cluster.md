# Running several servers

Several photon-server processes, nodes, can serve one household as one server. They share its
PostgreSQL and Valkey, and each reads the libraries from its own mount of the media. A client may
reach any node: a request for a stream another node runs is handed to that node, so each node
must be reachable by the others.

## Setting a node up

| Variable | |
|---|---|
| `PHOTON_NODE_ADDRESS` | where the other nodes reach this one, such as `http://10.0.0.5:8640`. A node without one is never handed another node's requests and is not listed. |

A node finds what it encodes video with as it starts: VideoToolbox on a Mac, NVENC, then QSV and
VAAPI on each render node, the first that encodes a test picture, or software where none does.
Its log says which, and Settings › Server shows it.

A node is named by its host. In a container, give each machine's its own name: the deploy
folder's compose file takes `PHOTON_HOSTNAME`, `photon` where it is unset.

Every node mounts the media at the same path, as a library is kept by its path. Node addresses
belong on a private network: the nodes trust what they hand each other.

Settings › Libraries checks a library's root on every node running, at
`POST /api/v1/admin/libraries/{id}/check`: the node asked asks each other at
`GET /api/v1/internal/libraries/{id}/check`, signed as a remux is. Each stats and lists the root,
and says it is `readable`, with how many entries it holds, or `missing`, `not_a_folder`, `denied`
or `unreadable`, with why; one that has not finished in 5 seconds, as on a hung network mount, is
`timed_out`, and a node that does not answer within 7 is `unreachable_node`.

## What each node tells the others

Every 15 seconds, and as soon as a transcode starts or ends, each node tells the others, through
Valkey, where it is reached, what it encodes with (its encoder, whether it encodes HEVC, whether
it can draw styled subtitles into video), and how many videos it encodes now of at most how
many. A node that says nothing for 45 seconds is no longer listed.

Settings › Server lists the nodes, what each transcodes with, and how busy each that is up is:
"3 of 8", with those transcoding for downloads said beneath.

## Where a transcode is made

A playback that encodes its video is made on the node with the most of its transcode slots free,
of those whose encoder can make it (HEVC, or styled subtitles drawn in, where it asks for them);
a node with no limit first. The node a client asks decides how the copy is played with that
node's encoder, starts the playback there, and answers the client: the client's every request
for its HLS, to whichever node, is handed to the node making it. A node with no slot free by the
time it is asked refuses, and the next is asked; when every one refuses, the client is told the
server is transcoding as many videos at once as it may, and how many that is across the nodes.

A playback that copies its video, or is played as it is, is made by the node asked.

Nodes ask each other at `POST /api/v1/internal/playbacks/{id}/remux`, signed with a key derived
from the cluster's own, which no client is ever given: a request no node signed, or changed on
its way, is refused. The route is no client's, and in no description of the API.

## What each node does

Settings › Server lists every node there is or has been. Each one's settings say what it does,
taken up at once, as it runs:

- **Role.** *All* serves clients and transcodes, as every node of a server of one does. *Serve
  only* serves clients and transcodes nothing, for its playbacks or for downloads: a node without
  a GPU beside one with. *Transcode first* is asked for a transcode before any node of all, where
  it has a slot free, and serves clients too: the GPU node.
- **Transcodes at once.** Worked out from the node's encoder, or set: at most a number, or no
  limit. Lowering it stops none playing; none is begun until there is room under it.

- **Drain.** A node drained takes no new stream to transcode and no download's conversion; its
  streams play to their end, and the others take new ones. Settings › Server says "Draining · 3
  streams left", then "Drained · safe to stop", with any note an admin left for the others.
  Resume gives it work again.

- **Forget.** A node not running stays listed, "Not running · last seen 3 days ago", with what an
  admin set of it, for it may only be restarting. One taken away for good is forgotten; should it
  start again, it joins as a new node does. A node running cannot be forgotten.

A node told to stop (SIGTERM, as `docker compose stop` and Kubernetes send) drains itself: it
tells the others at once, takes no new work, and goes on serving and telling the others where it
is until its streams have played to their end, two hours at most, before it stops. Meanwhile
`/readyz` answers 503, so a balancer that asks sends it no new clients, while what its streams
ask, landing on the others, is handed on to it. Telling it to
stop again stops it at once. Its container must be given that long to stop: the deploy folder's
compose file gives it `stop_grace_period: 2h`, and Kubernetes' `terminationGracePeriodSeconds`
does the same there.

A new node starts as all, its limit worked out, taking work. A node set to serve only, asked to transcode by
another told of it before the change, refuses, and the next is asked.

Worked out, a node encoding in software takes one transcode per four logical CPUs, and one
encoding on hardware takes 8. That is the NVENC sessions a GeForce card's driver allowed at once
before 591.44 (December 2025); 591.44 and later allow 12, per machine rather than per card,
which a limit set to 12 takes up. NVIDIA's professional cards, and the other encoders, have no
such count: they are bound by how fast they encode, which at 1080p reaches 8 or more. A
download's conversion takes a transcode slot only while no playback wants it.

## When a node is missing

A node missing from Settings › Server, while running, has stopped telling the others of itself:
it has no `PHOTON_NODE_ADDRESS`, or it cannot reach Valkey. Its log says which.

## Metrics

Every node answers `GET /metrics` on its own port in Prometheus' text format, to a client on the
server's local networks (Settings › Network; this machine's and the private ones where none is
set) and to no other: anyone else is answered 404. Behind a proxy, a client is known by the
proxy's `X-Forwarded-For` only where `PHOTON_TRUSTED_PROXIES` trusts it. Scrape each node.

Each node says what is its own:

| Metric | |
|---|---|
| `photon_build_info{version}` | the version it runs |
| `photon_node_info{node, role, state}` | its name, its role, and `active` or `draining` |
| `photon_playbacks{method}` | the playbacks it serves now: `direct`, `remux` or `transcode` |
| `photon_playback_starts_total{method}` | playbacks started by clients asking it, wherever each is served; one refused is not counted |
| `photon_sent_bytes_total{delivery}` | the bytes of media it sent players, a `file` as it is or HLS `segment`s (playlists among them), by either API; a request handed to another node is counted there |
| `photon_transcodes{kind}` | the videos it encodes now, for a `playback` or a download's `conversion` |
| `photon_transcode_slots` | the most it encodes at once; absent with no limit |
| `photon_transcode_refusals_total{reason}` | plays it refused for want of a node to encode them: every one `full`, or `no_encoder` taking it |
| `photon_hls_segment_wait_seconds` | how long a request for a segment waits for it to be made |
| `photon_jobs_finished_total{kind, outcome}` | runs of jobs it ended, `done` or `failed` (a failed run may be tried again) |

beside the Go runtime's (`go_*`) and the process's (`process_*`). These are summed across the
nodes: `sum(photon_playbacks)` is every playback going on.

What the nodes share, in Postgres and Valkey, is said only by the node holding the scheduler lease,
the one running scheduled tasks; the others leave it out, so it is never counted twice:

| Metric | |
|---|---|
| `photon_jobs{kind, state}` | the jobs queued, running, to run again (`rerun`) and `dead` |
| `photon_jobs_oldest_due_seconds{kind}` | how long the queued job due now and waiting longest has been due; one held for the maintenance window, or put off until later, is left out |
| `photon_task_last_finished_timestamp_seconds{task, result}` | when each task's last run ended, `succeeded`, `failed` or `cancelled`; absent while it runs again |
| `photon_nodes{state}` | the nodes running, `active` or `draining`, the leader among them whether or not it has an address |
| `photon_library_items{kind}` | the films (`movie`) and episodes in the libraries |
| `photon_library_bytes{kind}` | the bytes those films and episodes hold on disk: each file once however many paths or episode numbers read it, and every copy, hd or ultra hd, of a title; a missing file holds none |

The lease passes to another node within 20 seconds of its holder stopping, so for a moment either
or neither says them. Query them across instances with `max without(instance)`, as
`max without(instance) (photon_jobs{state="queued"})`, never `sum`.

An admin is answered what every node's metrics say now, and what the nodes share, at
`GET /api/v1/admin/metrics`: the node asked asks each other at `GET /api/v1/internal/metrics`,
signed as a remux is, and lists one that does not answer within 2 seconds as unreachable.
Settings › Server › Metrics shows it, asked every 5 seconds while the page is in view, with the
last ten minutes drawn beside it; history longer than that is Prometheus'.
