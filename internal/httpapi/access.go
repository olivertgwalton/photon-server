package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"
)

// rootAccess is whether a node can reach and read a library's root.
type rootAccess string

const (
	accessReadable   rootAccess = "readable"
	accessMissing    rootAccess = "missing"
	accessNotAFolder rootAccess = "not_a_folder"
	accessDenied     rootAccess = "denied"
	// accessUnreadable is any other failure to stat or list the root: an I/O error, a stale mount.
	accessUnreadable rootAccess = "unreadable"
	// accessTimedOut is a root not stat'd and listed within checkWithin, as a hung network or
	// FUSE mount leaves it.
	accessTimedOut rootAccess = "timed_out"
	// accessUnreachableNode is a node that did not answer within its time, or answered an error.
	accessUnreachableNode rootAccess = "unreachable_node"
)

func rootAccesses() []rootAccess {
	return []rootAccess{accessReadable, accessMissing, accessNotAFolder, accessDenied, accessUnreadable, accessTimedOut, accessUnreachableNode}
}

// checkWithin is how long a node has to stat and list a root before it is reported timed out.
const checkWithin = 5 * time.Second

// libraryCheckPath is where a node is asked by another whether it can read a library's root.
const libraryCheckPath = "/api/v1/internal/libraries/{id}/check"

// rootCheckJSON is what a node found of a library's root: entries is how many it lists, where it
// is readable, and error why it is not.
type rootCheckJSON struct {
	Access  rootAccess `json:"access"`
	Entries int        `json:"entries"`
	Error   string     `json:"error,omitzero"`
}

// libraryCheckJSON is whether every node running can read a library's root, by name.
type libraryCheckJSON struct {
	Root  string              `json:"root"`
	Nodes []nodeRootCheckJSON `json:"nodes"`
}

type nodeRootCheckJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	rootCheckJSON
}

// rootReads reads roots for the checks of them, one read of a root at a time.
type rootReads struct {
	group singleflight.Group
	read  func(root string) rootCheckJSON
}

// check reads root as the scanner does, giving up after checkWithin or when ctx is done.
func (rr *rootReads) check(ctx context.Context, root string) rootCheckJSON {
	// On a hung mount the read is stuck in the kernel and outlives the check, and the request,
	// that started it, until the mount answers. A check of the root meanwhile waits on that read
	// rather than leaving another stuck beside it.
	read := rr.group.DoChan(root, func() (any, error) { return rr.read(root), nil })
	timer := time.NewTimer(checkWithin)
	defer timer.Stop()
	select {
	case r := <-read:
		return r.Val.(rootCheckJSON) //nolint:forcetypeassert // the read answers nothing else
	case <-timer.C:
	case <-ctx.Done():
	}
	return rootCheckJSON{Access: accessTimedOut, Error: fmt.Sprintf("%s did not answer within %s", root, checkWithin)}
}

func readRoot(root string) rootCheckJSON {
	info, err := os.Stat(root)
	if err == nil && !info.IsDir() {
		return rootCheckJSON{Access: accessNotAFolder, Error: root + " is not a folder"}
	}
	var entries []os.DirEntry
	if err == nil {
		entries, err = os.ReadDir(root)
	}
	switch {
	case err == nil:
		return rootCheckJSON{Access: accessReadable, Entries: len(entries)}
	case errors.Is(err, fs.ErrNotExist):
		return rootCheckJSON{Access: accessMissing, Error: err.Error()}
	case errors.Is(err, fs.ErrPermission):
		return rootCheckJSON{Access: accessDenied, Error: err.Error()}
	default:
		return rootCheckJSON{Access: accessUnreadable, Error: err.Error()}
	}
}

// nodeCheckLibrary answers another node whether this one can read a library's root.
func (a *API) nodeCheckLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	lib, err := a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.reads.check(r.Context(), lib.Root))
}

// checkLibrary answers whether every node running can read a library's root: any node may scan it
// or play from it.
func (a *API) checkLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	lib, err := a.svc.Libraries.Library(ctx, id)
	if a.answered(w, r, err) {
		return
	}
	// A node has as long again as a metrics answer beyond its check, so a root it reports timed
	// out is not taken for the node itself out of reach.
	answers, err := fromEveryNode(ctx, a, strings.Replace(libraryCheckPath, "{id}", id.String(), 1), checkWithin+gatherWithin,
		func() rootCheckJSON { return a.reads.check(ctx, lib.Root) })
	if a.answered(w, r, err) {
		return
	}
	out := libraryCheckJSON{Root: lib.Root, Nodes: make([]nodeRootCheckJSON, len(answers))}
	for i, ans := range answers {
		out.Nodes[i] = nodeRootCheckJSON{ID: ans.Node.ID, Name: ans.Node.Name, rootCheckJSON: ans.Answer}
		if ans.Err != nil {
			out.Nodes[i].rootCheckJSON = rootCheckJSON{Access: accessUnreachableNode, Error: ans.Err.Error()}
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func (a *API) accessRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/admin/libraries/{id}/check", access: admin,
			summary: "Check that every node running can reach and read a library's root, as any may scan or play from it",
			status:  http.StatusOK, reply: libraryCheckJSON{}, handle: a.checkLibrary,
		},
	}
}
