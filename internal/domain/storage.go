package domain

import (
	"time"
	"uuid"
)

// StorageKind is where artwork, avatars, theme tunes and previews are kept.
type StorageKind string

const (
	// StorageDisk is each node's own cache folder.
	StorageDisk StorageKind = "disk"
	// StorageBucket is an S3 bucket every node shares.
	StorageBucket StorageKind = "bucket"
)

func StorageKinds() []StorageKind {
	return []StorageKind{StorageDisk, StorageBucket}
}

// Delivery is how clients are given what is kept in a bucket.
type Delivery string

const (
	// DeliverProxy sends it through the server, for a bucket clients cannot reach.
	DeliverProxy Delivery = "proxy"
	// DeliverRedirect sends clients to read pictures, sounds and previews from the bucket itself.
	DeliverRedirect Delivery = "redirect"
)

func Deliveries() []Delivery {
	return []Delivery{DeliverProxy, DeliverRedirect}
}

// Storage is where an admin has chosen artwork and previews be kept.
type Storage struct {
	Kind StorageKind
	// Bucket is the bucket, where Kind is StorageBucket.
	Bucket Bucket
}

// Bucket is an S3 bucket and how it is reached.
type Bucket struct {
	// Endpoint is the store the bucket is in, as https://host[:port]; "" is Amazon S3.
	Endpoint string
	Name     string
	// Folder is where in the bucket things are kept; "" is its root.
	Folder string
	// Region is the bucket's; "" asks the store.
	Region string
	// AccessKey and SecretKey sign each request; without them, AWS's own credentials do.
	AccessKey, SecretKey string
	Delivery             Delivery
	// PublicEndpoint is where clients reach the store, where it is not at Endpoint.
	PublicEndpoint string
}

// SamePlace reports whether s keeps things where o does, whatever it signs with.
func (s Storage) SamePlace(o Storage) bool {
	if s.Kind != o.Kind {
		return false
	}
	switch s.Kind {
	case StorageDisk:
		return true
	case StorageBucket:
		return s.Bucket.Endpoint == o.Bucket.Endpoint && s.Bucket.Name == o.Bucket.Name && s.Bucket.Folder == o.Bucket.Folder
	}
	return false
}

// StorageMove is artwork and previews being moved to another place. Every node writes to both
// until each place they are copied from is done, and then keeps them in To.
type StorageMove struct {
	To      Storage
	Started time.Time
	Sources []MoveSource
}

// MoveSource is what one copy is from: a node's own disk, or with no Node the bucket all share.
type MoveSource struct {
	Node uuid.UUID
	// Copied of Total are what is copied so far, of what there was when it was listed; Total is
	// zero until then.
	Copied, Total int
	Done          bool
	// Seen is when it last said how far it had got.
	Seen time.Time
}

// Shared reports whether the moving of from to to is one copy, from a bucket every node shares to
// another, rather than one copy per node of its own disk.
func Shared(from, to Storage) bool {
	return from.Kind == StorageBucket && to.Kind == StorageBucket
}
