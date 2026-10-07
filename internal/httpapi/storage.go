package httpapi

import (
	"cmp"
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type storageSettings interface {
	Storage(ctx context.Context) (domain.Storage, error)
	SetStorage(ctx context.Context, s domain.Storage) error
	StorageMove(ctx context.Context) (domain.StorageMove, bool, error)
	StartStorageMove(ctx context.Context, to domain.Storage) error
	CancelStorageMove(ctx context.Context) error
}

// stores are this node's artwork and previews, where they are kept now.
type stores interface {
	Check(ctx context.Context, b domain.Bucket) error
	Probe(ctx context.Context) (string, error)
}

// storageJSON is where artwork, avatars, theme tunes and previews are kept: disk, each server's
// own cache folder; bucket, an S3 bucket every server shares.
type storageJSON struct {
	Kind   domain.StorageKind `json:"kind"`
	Bucket bucketJSON         `json:"bucket,omitzero"`
}

// bucketJSON is an S3 bucket. Endpoint is its store's address, as https://host[:port], or none for
// Amazon S3; folder is where in the bucket things are kept, or none for its root; region is asked
// of the store when none is given. Without an access key, the server's own AWS credentials sign:
// its environment, shared credentials file or instance role. The secret key is written and never
// read back: one left out keeps the one kept for the same access key. Delivery is how clients are
// given what is kept: proxy, through this server, as when it is left out; redirect, sent to read
// pictures, sounds and previews from the bucket, at public_endpoint where they reach the store at
// another address than endpoint.
type bucketJSON struct {
	Endpoint       string          `json:"endpoint,omitzero"`
	Name           string          `json:"name"`
	Folder         string          `json:"folder,omitzero"`
	Region         string          `json:"region,omitzero"`
	AccessKey      string          `json:"access_key,omitzero"`
	SecretKey      string          `json:"secret_key,omitzero"`
	Delivery       domain.Delivery `json:"delivery,omitzero"`
	PublicEndpoint string          `json:"public_endpoint,omitzero"`
}

// storageStatusJSON is where things are kept, saying whether a secret key is kept and never what,
// and the move to elsewhere under way, if one is.
type storageStatusJSON struct {
	Kind   domain.StorageKind `json:"kind"`
	Bucket *bucketStatusJSON  `json:"bucket,omitempty"`
	Move   *storageMoveJSON   `json:"move,omitempty"`
}

// storageMoveJSON is a move of what is kept to another place. Every server writes to both until
// each copy is done, and then keeps things there. A copy is of one server's own disk, or, with
// no node, of the bucket every server shares.
type storageMoveJSON struct {
	To      storageStatusJSON `json:"to"`
	Started time.Time         `json:"started"`
	Copies  []moveCopyJSON    `json:"copies"`
}

// moveCopyJSON is how far one copy has got: copied of total, what there was when it was listed,
// which is zero until then; seen is when it last said so.
type moveCopyJSON struct {
	Node   uuid.UUID `json:"node,omitzero"`
	Copied int       `json:"copied"`
	Total  int       `json:"total"`
	Done   bool      `json:"done"`
	Seen   time.Time `json:"seen"`
}

func showMove(m domain.StorageMove) *storageMoveJSON {
	out := &storageMoveJSON{To: showStorage(m.To), Started: m.Started, Copies: make([]moveCopyJSON, len(m.Sources))}
	for i, src := range m.Sources {
		out.Copies[i] = moveCopyJSON{Node: src.Node, Copied: src.Copied, Total: src.Total, Done: src.Done, Seen: src.Seen}
	}
	return out
}

// bucketStatusJSON is the bucket things are kept in. Probe, where clients are sent to the bucket,
// is a link to a picture there, for a browser to find whether it reaches the bucket: once the page
// may draw from it.
type bucketStatusJSON struct {
	Endpoint       string          `json:"endpoint,omitzero"`
	Name           string          `json:"name"`
	Folder         string          `json:"folder,omitzero"`
	Region         string          `json:"region,omitzero"`
	AccessKey      string          `json:"access_key,omitzero"`
	SecretKeySet   bool            `json:"secret_key_set"`
	Delivery       domain.Delivery `json:"delivery"`
	PublicEndpoint string          `json:"public_endpoint,omitzero"`
	Probe          string          `json:"probe,omitzero"`
}

func showStorage(s domain.Storage) storageStatusJSON {
	out := storageStatusJSON{Kind: s.Kind}
	switch s.Kind {
	case domain.StorageBucket:
		b := s.Bucket
		out.Bucket = &bucketStatusJSON{
			Endpoint: b.Endpoint, Name: b.Name, Folder: b.Folder, Region: b.Region,
			AccessKey: b.AccessKey, SecretKeySet: b.SecretKey != "", Delivery: b.Delivery,
			PublicEndpoint: b.PublicEndpoint,
		}
	case domain.StorageDisk:
	}
	return out
}

func (a *API) adminStorage(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Storage.Storage(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := showStorage(s)
	m, moving, err := a.svc.Storage.StorageMove(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if moving {
		out.Move = showMove(m)
	}
	if out.Bucket != nil {
		// This node's store is the one an admin chose, but for the minute a change takes to reach it.
		if out.Bucket.Probe, err = a.svc.Stores.Probe(r.Context()); err != nil {
			a.internal(w, r, err)
			return
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// storageOf is where req says things be kept, keeping the secret key kept now for its access key
// where req leaves it out; false, answered, where it says nothing whole.
func (a *API) storageOf(w http.ResponseWriter, req storageJSON, now domain.Storage) (domain.Storage, bool) {
	switch req.Kind {
	case domain.StorageDisk:
		return domain.Storage{Kind: domain.StorageDisk}, true
	case domain.StorageBucket:
	}
	b := domain.Bucket{
		Endpoint: req.Bucket.Endpoint, Name: req.Bucket.Name, Folder: req.Bucket.Folder, Region: req.Bucket.Region,
		AccessKey: req.Bucket.AccessKey, SecretKey: req.Bucket.SecretKey,
		Delivery: cmp.Or(req.Bucket.Delivery, domain.DeliverProxy), PublicEndpoint: req.Bucket.PublicEndpoint,
	}
	if b.SecretKey == "" && b.AccessKey != "" && b.AccessKey == now.Bucket.AccessKey {
		b.SecretKey = now.Bucket.SecretKey
	}
	switch {
	case b.Name == "":
		writeProblem(w, a.logger, codeInvalidBody, "a bucket needs its name")
	case (b.AccessKey == "") != (b.SecretKey == ""):
		writeProblem(w, a.logger, codeInvalidBody, "an access key needs its secret key, and a secret key its access key")
	default:
		return domain.Storage{Kind: domain.StorageBucket, Bucket: b}, true
	}
	return domain.Storage{}, false
}

// setStorage keeps artwork and previews where req says, once a bucket is checked. Signing for the
// same place differently is taken up by every node at once; another place is moved to, every node
// copying what it keeps, and the answer says the move has begun.
func (a *API) setStorage(w http.ResponseWriter, r *http.Request) {
	var req storageJSON
	if !a.decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	now, err := a.svc.Storage.Storage(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if _, moving, err := a.svc.Storage.StorageMove(ctx); err != nil || moving {
		if !a.answered(w, r, err) {
			writeProblem(w, a.logger, codeConflict, "what is kept is being moved: cancel the move first")
		}
		return
	}
	s, ok := a.storageOf(w, req, now)
	if !ok {
		return
	}
	if s.Kind == domain.StorageBucket {
		if err := a.svc.Stores.Check(ctx, s.Bucket); err != nil {
			writeProblem(w, a.logger, codeInvalidBody, err.Error())
			return
		}
	}
	out := showStorage(s)
	if s.SamePlace(now) {
		err = a.svc.Storage.SetStorage(ctx, s)
	} else {
		out = showStorage(now)
		out.Move = showMove(domain.StorageMove{To: s, Started: time.Now()})
		err = a.svc.Storage.StartStorageMove(ctx, s)
	}
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// cancelStorageMove stops a move, every node keeping things where they were; what was copied stays
// where it was copied to.
func (a *API) cancelStorageMove(w http.ResponseWriter, r *http.Request) {
	if a.answered(w, r, a.svc.Storage.CancelStorageMove(r.Context())) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventStorageChanged})
	w.WriteHeader(http.StatusNoContent)
}

// checkStorage reports why a bucket cannot keep artwork and previews, before it is chosen.
func (a *API) checkStorage(w http.ResponseWriter, r *http.Request) {
	var req storageJSON
	if !a.decode(w, r, &req) {
		return
	}
	now, err := a.svc.Storage.Storage(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	s, ok := a.storageOf(w, req, now)
	if !ok {
		return
	}
	if s.Kind == domain.StorageBucket {
		if err := a.svc.Stores.Check(r.Context(), s.Bucket); err != nil {
			writeProblem(w, a.logger, codeInvalidBody, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
