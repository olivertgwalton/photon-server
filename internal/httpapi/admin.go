package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// requireAdmin admits a signed-in admin.
func (a *API) requireAdmin(next http.Handler) http.Handler {
	return a.requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionOf(r).Profile.Role != domain.RoleAdmin {
			writeProblem(w, a.logger, codeForbidden, "only an admin may")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type libraryAdmin interface {
	Libraries(ctx context.Context) ([]domain.Library, error)
	Library(ctx context.Context, id uuid.UUID) (domain.Library, error)
	AddLibrary(ctx context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error)
	SetLibrary(ctx context.Context, id uuid.UUID, change store.LibraryChange) error
	RemoveLibrary(ctx context.Context, id uuid.UUID) error
	ScanLibrary(ctx context.Context, lib uuid.UUID, delay time.Duration) error
}

// adminLibraryJSON is a library as an admin sees it: where it is and how it is kept.
type adminLibraryJSON struct {
	ID           uuid.UUID            `json:"id"`
	Name         string               `json:"name"`
	Kind         domain.LibraryKind   `json:"kind"`
	Root         string               `json:"root"`
	Sources      []domain.FieldSource `json:"sources"`
	RemoteExtras []domain.ExtraKind   `json:"remote_extras"`
	Monitor      domain.Monitor       `json:"monitor"`
	RefreshDays  int                  `json:"refresh_days"`
}

func adminLibrary(l domain.Library) adminLibraryJSON {
	return adminLibraryJSON{
		ID: l.ID, Name: l.Name, Kind: l.Kind, Root: l.Root, Sources: nonNil(l.Sources),
		RemoteExtras: nonNil(l.RemoteExtras), Monitor: l.Monitor, RefreshDays: l.RefreshDays,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (a *API) adminLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := a.svc.Libraries.Libraries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]adminLibraryJSON, len(libs))
	for i, l := range libs {
		out[i] = adminLibrary(l)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}

// addLibrary adds a library of a folder on the server and scans it at once.
func (a *API) addLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string             `json:"name"`
		Kind domain.LibraryKind `json:"kind"`
		Root string             `json:"root"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	if _, err := domain.ParseLibraryKind(string(req.Kind)); err != nil || req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set and kind is movies or shows")
		return
	}
	if !filepath.IsAbs(req.Root) {
		writeProblem(w, a.logger, codeInvalidBody, "root is an absolute path")
		return
	}
	root := filepath.Clean(req.Root)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		writeProblem(w, a.logger, codeInvalidBody, "root is not a folder the server can read")
		return
	}
	lib, err := a.svc.Libraries.AddLibrary(r.Context(), req.Name, req.Kind, root)
	if errors.Is(err, store.ErrLibraryExists) {
		writeProblem(w, a.logger, codeConflict, "a library has that name or root")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if err := a.svc.Libraries.ScanLibrary(r.Context(), lib.ID, 0); err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, adminLibrary(lib))
}

// setLibrary changes what is sent of a library: its name, whether it is watched, where its
// metadata comes from, and the kinds of video it keeps providers' links to.
func (a *API) setLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name         string               `json:"name"`
		Sources      []domain.FieldSource `json:"sources"`
		RemoteExtras []domain.ExtraKind   `json:"remote_extras"`
		Monitor      domain.Monitor       `json:"monitor"`
		RefreshDays  *int                 `json:"refresh_days"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	change := store.LibraryChange{Name: req.Name, Sources: req.Sources, RemoteExtras: req.RemoteExtras, Monitor: req.Monitor, RefreshDays: req.RefreshDays}
	if d := req.RefreshDays; d != nil && (*d < 0 || *d > 365) {
		writeProblem(w, a.logger, codeInvalidBody, "refresh_days is from 0, never, to 365")
		return
	}
	if err := validChange(change); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	err := a.svc.Libraries.SetLibrary(r.Context(), id, change)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
		return
	case errors.Is(err, store.ErrLibraryExists):
		writeProblem(w, a.logger, codeConflict, "a library has that name")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	lib, err := a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, adminLibrary(lib))
}

// validChange checks each list against its enum, as the command line's parsers do, sources for
// repeats too.
func validChange(c store.LibraryChange) error {
	if len(c.Sources) > 0 {
		if _, err := domain.ParseMetadataSources(join(c.Sources)); err != nil {
			return err
		}
	}
	if len(c.RemoteExtras) > 0 {
		if _, err := domain.ParseExtraKinds(join(c.RemoteExtras)); err != nil {
			return err
		}
	}
	if c.Monitor != "" {
		if _, err := domain.ParseMonitor(string(c.Monitor)); err != nil {
			return err
		}
	}
	return nil
}

func join[T ~string](list []T) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = string(v)
	}
	return strings.Join(s, ",")
}

func (a *API) removeLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Libraries.RemoveLibrary(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// scanLibrary queues a scan of a library now.
func (a *API) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.svc.Libraries.Library(r.Context(), id); a.answered(w, r, err) {
		return
	}
	if err := a.svc.Libraries.ScanLibrary(r.Context(), id, 0); err != nil {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return uuid.UUID{}, false
	}
	return id, true
}
