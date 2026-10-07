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
| `PHOTON_MAX_TRANSCODES` | how many videos it encodes at once, a number or `unlimited`; without it, worked out from the encoder |

Every node mounts the media at the same path, as a library is kept by its path. Node addresses
belong on a private network: the nodes trust what they hand each other.

## What each node tells the others

Every 15 seconds, and as soon as a transcode starts or ends, each node tells the others, through
Valkey, where it is reached, what it encodes with (its encoder, whether it encodes HEVC, whether
it can draw styled subtitles into video), and how many videos it encodes now of at most how
many. A node that says nothing for 45 seconds is no longer listed.

Settings › Server lists the nodes, what each transcodes with, and how busy it is: "3 of 8", with
those transcoding for downloads said beneath.

## How many transcodes at once

Without `PHOTON_MAX_TRANSCODES`, a node encoding in software takes one transcode per four logical
CPUs, and one encoding on hardware takes 8. That is the NVENC sessions a GeForce card's driver
allowed at once before 591.44 (December 2025); 591.44 and later allow 12, per machine rather than
per card, which `PHOTON_MAX_TRANSCODES=12` takes up. NVIDIA's professional cards, and the other
encoders, have no such count: they are bound by how fast they encode, which at 1080p reaches 8
or more. A download's conversion takes a transcode slot only while no playback wants it.

## When a node is missing

A node missing from Settings › Server, while running, has stopped telling the others of itself:
it has no `PHOTON_NODE_ADDRESS`, or it cannot reach Valkey. Its log says which.
