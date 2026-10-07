package domain

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
