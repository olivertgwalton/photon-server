package domain

import (
	"errors"
	"time"
	"uuid"
)

// Node is a server node as it tells the others of itself: where its peers reach it, what it
// encodes with, and how many videos it encodes now, said every little while and as that changes.
type Node struct {
	ID      uuid.UUID
	Address string
	Seen    time.Time
	// Name is its host's, to tell nodes apart by.
	Name         string
	Group        string
	PublicURL    string
	LocalURL     string
	Role         NodeRole
	Availability NodeAvailability
	Encoder      Encoder
	// Transcodes are the videos it encodes now, Conversions of them for downloads, of at most
	// Limit at once; a Limit of zero is none.
	Transcodes, Conversions, Limit int
	LimitSource                    LimitSource
	// Streams are the playbacks it delivers now, encoded or not, of at most StreamLimit at once;
	// a StreamLimit of zero is none.
	Streams, StreamLimit int
	// EgressKbps is how fast it sends its players media, as last measured, of at most
	// EgressLimitMbps; an EgressLimitMbps of zero is none.
	EgressKbps, EgressLimitMbps int
}

// ClientNetwork is which of the server's networks a client is on, which says where it reaches a
// node.
type ClientNetwork string

const (
	// NetworkLocal is one of the server's local networks, as Settings › Network lists them.
	NetworkLocal  ClientNetwork = "local"
	NetworkRemote ClientNetwork = "remote"
)

// URLFor is where a client on network reaches n itself: its local URL, or its public URL where it
// has none, for one on a local network, and its public URL for one elsewhere. Empty is nowhere.
func (n Node) URLFor(network ClientNetwork) string {
	switch network {
	case NetworkLocal:
		if n.LocalURL != "" {
			return n.LocalURL
		}
	case NetworkRemote:
	}
	return n.PublicURL
}

// NodeRole is what a node does for the cluster, as an admin sets it.
type NodeRole string

const (
	// NodeAll serves clients and encodes video: every node of a cluster of one is.
	NodeAll NodeRole = "all"
	// NodeServe serves clients and never encodes video, leaving that to the others: a node
	// without a GPU beside one with.
	NodeServe NodeRole = "serve"
	// NodeTranscode encodes video before any node of all does, and serves clients too.
	NodeTranscode NodeRole = "transcode"
)

func NodeRoles() []NodeRole {
	return []NodeRole{NodeAll, NodeServe, NodeTranscode}
}

// ErrStopping is a node stopping, which draining is not ready for new clients.
var ErrStopping = errors.New("the node is stopping, its streams playing to their end")

// NodeAvailability is whether a node takes new work: an admin drains one before stopping it, or
// while its GPU's driver is updated, and its streams play to their end meanwhile.
type NodeAvailability string

const (
	NodeActive   NodeAvailability = "active"
	NodeDraining NodeAvailability = "draining"
)

func NodeAvailabilities() []NodeAvailability {
	return []NodeAvailability{NodeActive, NodeDraining}
}

// Takes reports whether a node of the availability takes new work.
func (a NodeAvailability) Takes() bool {
	switch a {
	case NodeActive:
		return true
	case NodeDraining:
	}
	return false
}

// Encodes reports whether a node of the role encodes video: for playbacks, and downloads.
func (r NodeRole) Encodes() bool {
	switch r {
	case NodeAll, NodeTranscode:
		return true
	case NodeServe:
	}
	return false
}

// Encoder is what a node encodes video with.
type Encoder struct {
	Acceleration Acceleration
	// Device is the render node a VAAPI or QSV encoder is on, or the index of the NVIDIA card.
	Device string
	HEVC   HEVCEncoding
	// Libass is whether it can draw styled subtitles into video.
	Libass bool
}

// LimitSource is where a node's limit on transcodes at once comes from.
type LimitSource string

const (
	// LimitAutomatic is what its encoder keeps up with.
	LimitAutomatic LimitSource = "automatic"
	// LimitSet is an admin's.
	LimitSet LimitSource = "set"
)

func LimitSources() []LimitSource {
	return []LimitSource{LimitAutomatic, LimitSet}
}

// NodeSettings are what an admin sets of a node: its role, with LimitSet its limit on transcodes at
// once, zero for none, and whether it takes new work.
type NodeSettings struct {
	Role        NodeRole
	LimitSource LimitSource
	Limit       int
	// Availability is whether it takes new work, and Note why, for other admins, where it does not.
	Availability NodeAvailability
	Note         string
	// Address is where the other nodes reach it, such as http://10.0.0.5:8640; none is a node
	// handed no one else's requests and not listed.
	Address string
	// PublicURL is where clients reach it themselves, such as https://gpu.example.com: a playback
	// it makes for another node's client is fetched from there, not handed on through that node.
	// None is a node clients reach only through the others.
	PublicURL string
	// LocalURL is where clients on the server's local networks reach it themselves, such as
	// http://192.168.1.5:8640, or its tailnet address where the tailnet is among them; none is a
	// node they reach at its public URL.
	LocalURL string
	// StreamLimit is how many playbacks it delivers at once, encoded or not; zero is no limit.
	StreamLimit int
	// EgressLimitMbps is how fast it sends its players media, in megabits a second, past which it
	// is given no more to deliver; zero is no limit.
	EgressLimitMbps int
	// Group is the nodes it is among, such as those at one site: a node asked to play what is
	// encoded asks those of its own group first.
	Group string
	// Encoding is what it encodes video on.
	Encoding EncoderChoice
}

// EncoderSource is where what a node encodes video on comes from.
type EncoderSource string

const (
	// EncoderAutomatic is the first the node finds that encodes a test picture.
	EncoderAutomatic EncoderSource = "automatic"
	// EncoderSet is an admin's.
	EncoderSet EncoderSource = "set"
)

func EncoderSources() []EncoderSource {
	return []EncoderSource{EncoderAutomatic, EncoderSet}
}

// EncoderChoice is what an admin sets a node to encode video on: with EncoderSet, an acceleration
// and the device it is on, none being the first of them.
type EncoderChoice struct {
	Source       EncoderSource
	Acceleration Acceleration
	Device       string
}

// NodeRecord is a node as the server keeps it, whether it is up or not.
type NodeRecord struct {
	ID                  uuid.UUID
	Name                string
	FirstSeen, LastSeen time.Time
	NodeSettings
}
