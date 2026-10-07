package store

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var ErrLibraryExists = errors.New("a library with that name or root already exists")

// libraryColumns and librarySourceColumns are model.Library's and model.LibrarySource's, for a
// statement that reads whole rows.
const (
	libraryColumns = `id, name, kind, root, monitor, refresh_days, previews, markers, keyframes, themes, deletion,
		metadata_language, certification_country, artwork_language, title_language, collection_mode`
	librarySourceColumns = `library_id, item_kind, fetcher, source, position, enabled`
)

// hasLibrary answers ErrNotFound for no such library.
func hasLibrary(ctx context.Context, q db, lib uuid.UUID) error {
	var one int
	return found(q.QueryRow(ctx, `SELECT 1 FROM libraries WHERE id = $1`, lib).Scan(&one))
}

func (s *Store) AddLibrary(ctx context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error) {
	var row model.Library
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		row, err = readRow[model.Library](ctx, tx, `
			INSERT INTO libraries (name, kind, root) VALUES ($1, $2, $3) RETURNING `+libraryColumns, name, kind, root)
		if err != nil {
			return err
		}
		if err := saveSources(ctx, tx, row.ID, domain.DefaultSources(kind)); err != nil {
			return err
		}
		return saveRemoteExtras(ctx, tx, row.ID, domain.DefaultRemoteExtras())
	})
	if violates(err, uniqueViolation) {
		return domain.Library{}, ErrLibraryExists
	}
	if err != nil {
		return domain.Library{}, fmt.Errorf("adding library: %w", err)
	}
	return library(row, domain.DefaultSources(kind), domain.DefaultRemoteExtras()), nil
}

func (s *Store) Libraries(ctx context.Context) ([]domain.Library, error) {
	rows, err := queryRows[model.Library](ctx, s.pool, `SELECT `+libraryColumns+` FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	taken, err := queryRows[model.LibrarySource](ctx, s.pool, `
		SELECT `+librarySourceColumns+` FROM library_sources ORDER BY position`)
	if err != nil {
		return nil, err
	}
	ranked := map[uuid.UUID]map[domain.ItemKind]map[domain.Fetcher][]domain.RankedSource{}
	for _, t := range taken {
		if ranked[t.LibraryID] == nil {
			ranked[t.LibraryID] = map[domain.ItemKind]map[domain.Fetcher][]domain.RankedSource{}
		}
		if ranked[t.LibraryID][t.ItemKind] == nil {
			ranked[t.LibraryID][t.ItemKind] = map[domain.Fetcher][]domain.RankedSource{}
		}
		ranked[t.LibraryID][t.ItemKind][t.Fetcher] = append(ranked[t.LibraryID][t.ItemKind][t.Fetcher], domain.RankedSource{Source: t.Source, Enabled: t.Enabled})
	}
	kept, err := queryRows[model.LibraryRemoteExtra](ctx, s.pool, `
		SELECT library_id, kind FROM library_remote_extras ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	extras := map[uuid.UUID][]domain.ExtraKind{}
	for _, k := range kept {
		extras[k.LibraryID] = append(extras[k.LibraryID], k.Kind)
	}
	libs := make([]domain.Library, len(rows))
	for i, r := range rows {
		var sources []domain.KindSources
		for _, kind := range r.Kind.ItemKinds() {
			by := ranked[r.ID][kind]
			sources = append(sources, domain.KindSources{Kind: kind, Metadata: by[domain.FetcherMetadata], Images: by[domain.FetcherImages]})
		}
		libs[i] = library(*r, sources, extras[r.ID])
	}
	return libs, nil
}

// Library answers one library, or ErrNotFound.
func (s *Store) Library(ctx context.Context, id uuid.UUID) (domain.Library, error) {
	libs, err := s.Libraries(ctx)
	if err != nil {
		return domain.Library{}, err
	}
	for _, l := range libs {
		if l.ID == id {
			return l, nil
		}
	}
	return domain.Library{}, ErrNotFound
}

// LibraryChange is what to change about a library; an empty name, monitor, previews, markers,
// keyframes or themes, or a nil list, is left as it is.
type LibraryChange struct {
	Name string
	// Sources replace the rankings they give, of a kind and for metadata or pictures; a nil one is
	// left as it is.
	Sources      []domain.KindSources
	RemoteExtras []domain.ExtraKind
	Monitor      domain.Monitor
	// RefreshDays, where set, is how often its titles are matched again; zero never.
	RefreshDays *int
	// Previews is what pictures it makes of its videos; parts are brought into line by the
	// previews backfill.
	Previews domain.PreviewLevel
	// Markers is how it finds intros and credits; seasons not yet compared are queued by the daily
	// marker detection.
	Markers domain.MarkerDetection
	// Keyframes is how its files' keyframes are found; the parts with none known are queued to be
	// read again as it says.
	Keyframes domain.KeyframeMode
	// Themes is where it finds theme tunes; taking ThemerrDB's asks it of every film and show.
	Themes domain.ThemeLookup
	// Deletion is whether an admin may delete its titles with their files.
	Deletion domain.MediaDeletion
	// Collections is how its wall shows its collections.
	Collections domain.CollectionMode
	// MetadataLanguage and CertificationCountry, where set, replace its locale's: "" is the
	// server's own. With either changed its titles are described again in it.
	MetadataLanguage     *string
	CertificationCountry *string
	// ArtworkLanguage is which of its titles' pictures it takes first; changing it describes them
	// again.
	ArtworkLanguage domain.ArtworkLanguage
	// TitleLanguage is which title it gives its films and shows; changing it describes them again.
	TitleLanguage domain.TitleLanguage
}

// SetLibrary renames a library, changes whether it is watched, where each kind's metadata and
// pictures come from and in what order, and which kinds of video it keeps providers' links to. With new sources or kinds
// its titles are matched again, and with new sources its folders are read again at the next scan
// too, so the new order reaches everything already there.
func (s *Store) SetLibrary(ctx context.Context, id uuid.UUID, change LibraryChange) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var keyframes domain.KeyframeMode
		var themes domain.ThemeLookup
		if err := tx.QueryRow(ctx, `SELECT keyframes, themes FROM libraries WHERE id = $1`, id).Scan(&keyframes, &themes); err != nil {
			return found(err)
		}
		set := func(column string, v any) error {
			_, err := tx.Exec(ctx, `UPDATE libraries SET `+column+` = $2 WHERE id = $1`, id, v)
			return err
		}
		if change.Name != "" {
			if err := set("name", change.Name); err != nil {
				return err
			}
		}
		if change.Sources != nil {
			for _, k := range change.Sources {
				for _, f := range domain.Fetchers() {
					if k.Of(f) == nil {
						continue
					}
					_, err := tx.Exec(ctx, `DELETE FROM library_sources WHERE library_id = $1 AND item_kind = $2 AND fetcher = $3`,
						id, k.Kind, f)
					if err != nil {
						return err
					}
				}
			}
			if err := saveSources(ctx, tx, id, change.Sources); err != nil {
				return err
			}
			// A show's match asks only about seasons not yet described, so those whose own
			// sources changed are described again.
			var deeper []string
			for _, k := range change.Sources {
				if k.Kind == domain.ItemSeason || k.Kind == domain.ItemEpisode {
					deeper = append(deeper, string(k.Kind))
				}
			}
			if len(deeper) > 0 {
				err := describeAgain(ctx, tx, `SELECT id FROM items WHERE library_id = @lib AND kind = ANY(@kinds)`,
					pgx.NamedArgs{"lib": id, "kinds": deeper})
				if err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1`, id); err != nil {
				return err
			}
		}
		if change.RefreshDays != nil {
			if err := set("refresh_days", *change.RefreshDays); err != nil {
				return err
			}
		}
		if change.Monitor != "" {
			if err := set("monitor", change.Monitor); err != nil {
				return err
			}
		}
		if change.Previews != "" {
			if err := set("previews", change.Previews); err != nil {
				return err
			}
		}
		if change.Markers != "" {
			if err := set("markers", change.Markers); err != nil {
				return err
			}
		}
		if change.Deletion != "" {
			if err := set("deletion", change.Deletion); err != nil {
				return err
			}
		}
		if change.Collections != "" {
			if err := set("collection_mode", change.Collections); err != nil {
				return err
			}
		}
		if change.Keyframes != "" && change.Keyframes != keyframes {
			if err := set("keyframes", change.Keyframes); err != nil {
				return err
			}
			if err := rekeyframe(ctx, tx, id, change.Keyframes); err != nil {
				return err
			}
		}
		if change.Themes != "" && change.Themes != themes {
			if err := set("themes", change.Themes); err != nil {
				return err
			}
			if change.Themes == domain.ThemesThemerr {
				if err := askThemes(ctx, tx, `SELECT id FROM items WHERE library_id = @lib`, pgx.NamedArgs{"lib": id}); err != nil {
					return err
				}
			}
		}
		relocated := false
		for column, v := range map[string]*string{"metadata_language": change.MetadataLanguage, "certification_country": change.CertificationCountry} {
			if v == nil {
				continue
			}
			tag, err := tx.Exec(ctx, `UPDATE libraries SET `+column+` = nullif($2, '') WHERE id = $1 AND `+column+` IS DISTINCT FROM nullif($2, '')`, id, *v)
			if err != nil {
				return err
			}
			relocated = relocated || tag.RowsAffected() > 0
		}
		if change.ArtworkLanguage != "" {
			tag, err := tx.Exec(ctx, `UPDATE libraries SET artwork_language = $2 WHERE id = $1 AND artwork_language <> $2`, id, change.ArtworkLanguage)
			if err != nil {
				return err
			}
			relocated = relocated || tag.RowsAffected() > 0
		}
		if change.TitleLanguage != "" {
			tag, err := tx.Exec(ctx, `UPDATE libraries SET title_language = $2 WHERE id = $1 AND title_language <> $2`, id, change.TitleLanguage)
			if err != nil {
				return err
			}
			relocated = relocated || tag.RowsAffected() > 0
		}
		if relocated {
			err := forgetDescriptions(ctx, tx, `SELECT id FROM items WHERE library_id = @lib AND parent_id IS NULL`, pgx.NamedArgs{"lib": id})
			if err != nil {
				return err
			}
			// Its seasons and episodes too, which are asked for only while they are named by their files.
			err = describeAgain(ctx, tx, `SELECT id FROM items WHERE library_id = @lib AND kind IN ('season', 'episode')`, pgx.NamedArgs{"lib": id})
			if err != nil {
				return err
			}
		}
		if change.Sources == nil && change.RemoteExtras == nil && !relocated {
			return nil
		}
		if change.RemoteExtras != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM library_remote_extras WHERE library_id = $1`, id); err != nil {
				return err
			}
			if err := saveRemoteExtras(ctx, tx, id, change.RemoteExtras); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT id FROM items WHERE library_id = $1 AND kind IN ('movie', 'show')`, id)
		if err != nil {
			return err
		}
		titles, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, t := range titles {
			if err := enqueue(ctx, tx, domain.JobIdentify, t); err != nil {
				return err
			}
		}
		return nil
	})
	if violates(err, uniqueViolation) {
		return ErrLibraryExists
	}
	return err
}

// RemoveLibrary forgets a library and everything in it; its files are left alone.
func (s *Store) RemoveLibrary(ctx context.Context, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM libraries WHERE id = $1`, id))
}

func saveRemoteExtras(ctx context.Context, tx db, lib uuid.UUID, kinds []domain.ExtraKind) error {
	if len(kinds) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, k := range kinds {
		b.Queue(`INSERT INTO library_remote_extras (library_id, kind) VALUES ($1, $2)`, lib, k)
	}
	return tx.SendBatch(ctx, b).Close()
}

func saveSources(ctx context.Context, tx db, lib uuid.UUID, sources []domain.KindSources) error {
	var rows []*model.LibrarySource
	for _, k := range sources {
		for _, f := range domain.Fetchers() {
			for n, r := range k.Of(f) {
				rows = append(rows, &model.LibrarySource{
					LibraryID: lib, ItemKind: k.Kind, Fetcher: f, Source: r.Source, Position: n, Enabled: r.Enabled,
				})
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := registered(ctx, tx, rows); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`
			INSERT INTO library_sources (library_id, item_kind, fetcher, source, position, enabled)
			VALUES ($1, $2, $3, $4, $5, $6)`, r.LibraryID, r.ItemKind, r.Fetcher, r.Source, r.Position, r.Enabled)
	}
	return tx.SendBatch(ctx, b).Close()
}

func library(r model.Library, sources []domain.KindSources, extras []domain.ExtraKind) domain.Library {
	return domain.Library{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Root: r.Root, Sources: sources, RemoteExtras: extras,
		Monitor: r.Monitor, RefreshDays: int(r.RefreshDays), Previews: r.Previews, Markers: r.Markers,
		Keyframes: r.Keyframes, Themes: r.Themes, Deletion: r.Deletion,
		Locale: domain.Locale{Language: deref(r.MetadataLanguage), Country: deref(r.CertificationCountry), Artwork: r.ArtworkLanguage},
		Titles: r.TitleLanguage, Collections: r.CollectionMode,
	}
}

// CertificateCountries answers the countries whose certificates the server can read, by ISO 3166-1
// alpha-2 code. The table also keeps ratings no country gives (Jellyfin's 0-PREFER), which are
// none.
func (s *Store) CertificateCountries(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT country FROM certificates WHERE country ~ '^[A-Z]{2}$' ORDER BY country`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetLibraryOrder puts a profile's libraries in this order. ErrNotFound for one named twice or no
// library.
func (s *Store) SetLibraryOrder(ctx context.Context, profile uuid.UUID, libs []uuid.UUID) error {
	if hasRepeats(libs) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM library_order WHERE profile_id = $1`, profile); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO library_order (profile_id, library_id, position)
			SELECT $1, l.id, o.position - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, position)
			JOIN libraries l ON l.id = o.id`, profile, libs)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != int64(len(libs)) {
			return ErrNotFound
		}
		return nil
	})
}
