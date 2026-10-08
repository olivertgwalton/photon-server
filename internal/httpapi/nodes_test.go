package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeNodes keeps the nodes there have been, and what is set of each.
type fakeNodes map[uuid.UUID]domain.NodeRecord

func (f fakeNodes) KnownNodes(context.Context) ([]domain.NodeRecord, error) {
	out := []domain.NodeRecord{}
	for _, n := range f {
		out = append(out, n)
	}
	return out, nil
}

func (f fakeNodes) Node(_ context.Context, id uuid.UUID) (domain.NodeRecord, error) {
	n, ok := f[id]
	if !ok {
		return n, store.ErrNotFound
	}
	return n, nil
}

func (f fakeNodes) ForgetNode(_ context.Context, id uuid.UUID) error {
	if _, ok := f[id]; !ok {
		return store.ErrNotFound
	}
	delete(f, id)
	return nil
}

func (f fakeNodes) SetNodeSettings(_ context.Context, id uuid.UUID, s domain.NodeSettings) error {
	n, ok := f[id]
	if !ok {
		return store.ErrNotFound
	}
	n.NodeSettings = s
	f[id] = n
	return nil
}

// An admin sees every node there has been: this one, up though it tells no other of itself; one
// up and busy; one that has stopped. What an admin sets of one is kept, and every node told.
func TestAnAdminSetsWhatEachNodeDoes(t *testing.T) {
	self, gpu, gone := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	seen := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	automatic := domain.NodeSettings{Role: domain.NodeAll, LimitSource: domain.LimitAutomatic, Availability: domain.NodeActive}
	nodes := fakeNodes{
		self: {ID: self, Name: "mini", FirstSeen: seen, NodeSettings: automatic},
		gpu:  {ID: gpu, Name: "gpu-1", FirstSeen: seen.Add(time.Hour), NodeSettings: automatic},
		gone: {ID: gone, Name: "old", FirstSeen: seen.Add(2 * time.Hour), NodeSettings: automatic},
	}
	events := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: events, Nodes: nodes,
		Valkey: fakeBackend{version: "9.0.0", nodes: []domain.Node{{
			ID: gpu, Address: "http://10.0.0.5:8640", Seen: seen, Name: "gpu-1", Role: domain.NodeAll, Availability: domain.NodeActive, Transcodes: 3, Limit: 8,
			LimitSource: domain.LimitAutomatic, Encoder: domain.Encoder{Acceleration: domain.AccelNVENC, HEVC: domain.HEVCAllow},
		}}},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node {
			return domain.Node{ID: self, Name: "mini", Role: domain.NodeAll, Availability: domain.NodeActive, Limit: 2, LimitSource: domain.LimitAutomatic}
		}, nil, nodecall.Key{}),
	})
	var listed listJSON[knownNodeJSON]
	if rec := ask(api, http.MethodGet, "/api/v1/admin/nodes", ""); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&listed) != nil {
		t.Fatalf("listing: %d %s", rec.Code, rec.Body)
	}
	up := map[uuid.UUID]*nodeJSON{}
	for _, n := range listed.Items {
		up[n.ID] = n.Online
	}
	if len(up) != 3 || up[self] == nil || up[gpu] == nil || up[gpu].Transcodes != 3 || up[gone] != nil {
		t.Errorf("listed %+v, want all three: this node and gpu-1 up, gpu-1 transcoding 3, the old one not", listed.Items)
	}

	rec := ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"role":"transcode","transcode_limit_source":"set","transcode_limit":12}`)
	want := domain.NodeSettings{Role: domain.NodeTranscode, LimitSource: domain.LimitSet, Limit: 12, Availability: domain.NodeActive}
	if rec.Code != http.StatusOK || nodes[gpu].NodeSettings != want {
		t.Fatalf("setting gpu-1: %d %s, kept %+v; want %+v", rec.Code, rec.Body, nodes[gpu].NodeSettings, want)
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventNodesChanged {
		t.Errorf("raised %+v, want every node told", events.raised)
	}
	// A change of role alone leaves the limit as it is set.
	ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"role":"serve"}`)
	if got := nodes[gpu].NodeSettings; got.Role != domain.NodeServe || got.Limit != 12 {
		t.Errorf("after a change of role: %+v, want serve with its limit kept", got)
	}
	for body, status := range map[string]int{
		`{"transcode_limit_source":"automatic"}`:                http.StatusOK,
		`{"role":"gpu"}`:                                        http.StatusBadRequest,
		`{"transcode_limit_source":"set"}`:                      http.StatusBadRequest,
		`{"transcode_limit_source":"set","transcode_limit":-1}`: http.StatusBadRequest,
	} {
		if rec := ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), body); rec.Code != status {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, status)
		}
	}
	if got := nodes[gpu].NodeSettings; got.LimitSource != domain.LimitAutomatic {
		t.Errorf("after setting it automatic: %+v", got)
	}
	// Its address is an origin the others put a path after, kept without a trailing slash, and
	// cleared by an empty one.
	ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"address":"http://10.0.0.6:8640/"}`)
	if got := nodes[gpu].Address; got != "http://10.0.0.6:8640" {
		t.Errorf("address kept as %q, want http://10.0.0.6:8640", got)
	}
	for _, bad := range []string{"10.0.0.6:8640", "ftp://10.0.0.6", "http://10.0.0.6:8640/api", "http://u:p@10.0.0.6"} {
		if rec := ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"address":"`+bad+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("address %q: %d, want 400", bad, rec.Code)
		}
	}
	ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"address":""}`)
	if got := nodes[gpu].Address; got != "" {
		t.Errorf("address cleared to %q", got)
	}
	// Drained with a note for other admins, then resumed, which clears it.
	ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"availability":"draining","note":"driver update"}`)
	if got := nodes[gpu].NodeSettings; got.Availability != domain.NodeDraining || got.Note != "driver update" || got.Role != domain.NodeServe {
		t.Errorf("drained: %+v, want draining with its note, its role kept", got)
	}
	if rec := ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"availability":"resting"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an availability there is not: %d, want 400", rec.Code)
	}
	ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+gpu.String(), `{"availability":"active"}`)
	if got := nodes[gpu].NodeSettings; got.Availability != domain.NodeActive || got.Note != "" {
		t.Errorf("resumed: %+v, want active, its note gone", got)
	}
	// A node up is not forgotten, as it would be back within the minute; one stopped is.
	for _, up := range []uuid.UUID{self, gpu} {
		if rec := ask(api, http.MethodDelete, "/api/v1/admin/nodes/"+up.String(), ""); rec.Code != http.StatusConflict {
			t.Errorf("forgetting a node up: %d, want 409", rec.Code)
		}
	}
	if rec := ask(api, http.MethodDelete, "/api/v1/admin/nodes/"+gone.String(), ""); rec.Code != http.StatusNoContent || len(nodes) != 2 {
		t.Errorf("forgetting the node stopped: %d, %d known; want it gone", rec.Code, len(nodes))
	}
	if rec := ask(api, http.MethodDelete, "/api/v1/admin/nodes/"+gone.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("forgetting it again: %d, want 404", rec.Code)
	}
	if rec := ask(api, http.MethodPatch, "/api/v1/admin/nodes/"+uuid.NewV7().String(), `{"role":"serve"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a node there has never been: %d, want 404", rec.Code)
	}
}
