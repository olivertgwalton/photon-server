package domain

import (
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
	Role         NodeRole
	Availability NodeAvailability
	Encoder      Encoder
	// Transcodes are the videos it encodes now, Conversions of them for downloads, of at most
	// Limit at once; a Limit of zero is none.
	Transcodes, Conversions, Limit int
	LimitSource                    LimitSource
}

// NodeRole is what a node does for the cluster, as PHOTON_ROLE says.
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
	HEVC         HEVCEncoding
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
}

// NodeRecord is a node as the server keeps it, whether it is up or not.
type NodeRecord struct {
	ID        uuid.UUID
	Name      string
	FirstSeen time.Time
	NodeSettings
}
