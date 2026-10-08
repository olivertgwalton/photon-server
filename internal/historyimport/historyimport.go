package historyimport

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// maxMisses is how many of the titles not imported an import lists; the rest are only counted.
const maxMisses = 200

type Imports struct {
	st *store.Store
}

func New(st *store.Store) *Imports {
	return &Imports{st: st}
}

// Start signs in to a source and queues the import of its user's history into a profile,
// answering its id, or ErrRefused for an address, credentials or profile that will not do.
func (i *Imports) Start(ctx context.Context, kind domain.ImportSource, address string, profile uuid.UUID, c Credentials) (uuid.UUID, error) {
	base, err := baseURL(address)
	if err != nil {
		return uuid.UUID{}, err
	}
	login, err := connect(ctx, kind, base, c)
	if err != nil {
		return uuid.UUID{}, err
	}
	id, err := i.st.AddImport(ctx, kind, base, profile, login)
	if err != nil {
		open(kind, base, login).signOut(ctx)
	}
	if errors.Is(err, store.ErrNotFound) {
		return id, fmt.Errorf("%w: profile is not one of the household's", ErrRefused)
	}
	return id, err
}

// Run is the job that imports one source's history. One its source fails ends failed, with why;
// one cut short runs again from the start, as what it wrote is written the same again.
func (i *Imports) Run(ctx context.Context, id uuid.UUID) error {
	h, login, err := i.st.StartImport(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	src := open(h.Source, h.URL, login)
	entries, err := src.entries(ctx)
	if err == nil {
		err = i.apply(ctx, &h, entries)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	src.signOut(ctx)
	h.Status = domain.ImportDone
	if err != nil {
		h.Status, h.Error = domain.ImportFailed, err.Error()
	}
	return i.st.FinishImport(ctx, h)
}

// apply writes what each entry says of the profile's state of the title it matches, where the
// source's is newer, counting what came of each.
func (i *Imports) apply(ctx context.Context, h *domain.HistoryImport, entries []entry) error {
	miss := func(e entry, reason domain.ImportMiss) {
		if len(h.Misses) < maxMisses {
			h.Misses = append(h.Misses, domain.Missed{Title: e.title, Reason: reason})
		}
	}
	for _, e := range entries {
		if len(e.ids) == 0 {
			h.Unmatched++
			miss(e, domain.MissNoIDs)
			continue
		}
		var item uuid.UUID
		var err error
		if e.kind == domain.ItemEpisode {
			item, err = i.st.MatchEpisode(ctx, e.ids, e.season, e.episode)
		} else {
			item, err = i.st.MatchFilm(ctx, e.ids)
		}
		if errors.Is(err, store.ErrNotFound) {
			h.Unmatched++
			miss(e, domain.MissNotFound)
			continue
		}
		if err != nil {
			return err
		}
		h.Matched++
		if e.at.IsZero() {
			h.Skipped++
			miss(e, domain.MissUndated)
			continue
		}
		wrote, err := i.write(ctx, h.Profile, item, e)
		if err != nil {
			return err
		}
		if wrote {
			h.Imported++
		} else {
			h.Skipped++
		}
	}
	return nil
}

// write marks a title watched, as often as the source played it, then where it was stopped, each
// unless the profile's state of it changed after the source's did.
func (i *Imports) write(ctx context.Context, profile, item uuid.UUID, e entry) (bool, error) {
	wrote := false
	if e.plays > 0 {
		err := i.st.ImportWatched(ctx, profile, item, e.plays, e.at)
		if err != nil && !errors.Is(err, store.ErrSuperseded) {
			return false, err
		}
		wrote = err == nil
	}
	if e.position > 0 {
		length, err := i.st.Length(ctx, item)
		if err != nil {
			return false, err
		}
		// As if it had already reached the end, so a position near it marks it watched without
		// counting another play.
		_, err = i.st.SaveProgress(ctx, profile, item, e.position, length, domain.ReachEnd, &e.at)
		if err != nil && !errors.Is(err, store.ErrSuperseded) {
			return false, err
		}
		wrote = wrote || err == nil
	}
	return wrote, nil
}

// baseURL is the address a source's paths are added to: http or https, with no credentials, query
// or fragment.
func baseURL(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: url is the server's http or https address, with no credentials or query", ErrRefused)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}
