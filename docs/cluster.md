# Running several servers

Several photon-server processes, nodes, can serve one household as one server. They share its
PostgreSQL and Valkey, and each reads the libraries from its own mount of the media. A client may
reach any node: a request for a stream another node runs is handed to that node, so each node
must be reachable by the others.

## Setting a node up

| Variable | |
|---|---|
| `PHOTON_NODE_ADDRESS` | where the other nodes reach this one, such as `http://10.0.0.5:8640`. A node without one is never handed another node's requests and is not listed. |
| `PHOTON_HWACCEL` | what it encodes video with: `software`, `nvenc`, `qsv`, `vaapi` or `videotoolbox` |
| `PHOTON_HWACCEL_DEVICE` | the device to encode on, such as `/dev/dri/renderD128` |

A node is named by its host. In a container, give each machine's its own name: the deploy
folder's compose file takes `PHOTON_HOSTNAME`, `photon` where it is unset.

Every node mounts the media at the same path, as a library is kept by its path. Node addresses
belong on a private network: the nodes trust what they hand each other.

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

A new node starts as all, its limit worked out. A node set to serve only, asked to transcode by
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
