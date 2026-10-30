package store

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

const artworkColumns = `id, item_id, source, kind, place, position, folder, language, width, height, blurhash`

// saveFolderArtwork replaces a title's pictures that are files in folder with those found there
// now; pictures it has in other folders stand.
func saveFolderArtwork(ctx context.Context, tx db, item uuid.UUID, folder string, pictures []domain.Artwork) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM artwork WHERE item_id = $1 AND source = $2 AND folder = $3`, item, domain.SourceFile, folder)
	rows := make([]*model.Artwork, 0, len(pictures))
	for n, p := range pictures {
		if path.Dir(p.Path) != folder {
			continue
		}
		rows = append(rows, &model.Artwork{
			ItemID: item, Source: domain.SourceFile, Kind: p.Kind, Place: p.Path, Position: n, Folder: &folder,
			Blurhash: optional(p.Blurhash),
		})
	}
	queueArtwork(b, rows)
	return tx.SendBatch(ctx, b).Close()
}

// saveProviderArtwork replaces what a provider has for a title with what it has now.
func saveProviderArtwork(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, pictures []domain.Artwork) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM artwork WHERE item_id = $1 AND source = $2`, item, source)
	rows := make([]*model.Artwork, len(pictures))
	for n, p := range pictures {
		rows[n] = &model.Artwork{
			ItemID: item, Source: source, Kind: p.Kind, Place: p.URL, Position: n,
			Language: optional(p.Language), Width: optional(p.Width), Height: optional(p.Height),
		}
	}
	queueArtwork(b, rows)
	return tx.SendBatch(ctx, b).Close()
}

func queueArtwork(b *pgx.Batch, rows []*model.Artwork) {
	if len(rows) == 0 {
		return
	}
	n := len(rows)
	items, positions := make([]uuid.UUID, n), make([]int, n)
	sources, kinds, places := make([]string, n), make([]string, n), make([]string, n)
	folders, languages, blurhashes := make([]*string, n), make([]*string, n), make([]*string, n)
	widths, heights := make([]*int, n), make([]*int, n)
	for i, r := range rows {
		items[i], sources[i], kinds[i], places[i], positions[i] = r.ItemID, string(r.Source), string(r.Kind), r.Place, r.Position
		folders[i], languages[i], widths[i], heights[i], blurhashes[i] = r.Folder, r.Language, r.Width, r.Height, r.Blurhash
	}
	// A provider may list one picture twice.
	b.Queue(`
		INSERT INTO artwork (item_id, source, kind, place, position, folder, language, width, height, blurhash)
		SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::smallint[], $6::text[], $7::text[],
			$8::int[], $9::int[], $10::text[])
		ON CONFLICT DO NOTHING`,
		items, sources, kinds, places, positions, folders, languages, widths, heights, blurhashes)
}

// Blurhashes are the BlurHashes of the pictures an answer carries, by picture id, for those that
// have one, as Jellyfin answers ImageBlurHashes beside its image tags.
type Blurhashes map[uuid.UUID]string

// blurhashesOf answers the BlurHashes in hashes of those of ids that have one.
func blurhashesOf(hashes map[uuid.UUID]string, ids ...uuid.UUID) Blurhashes {
	var out Blurhashes
	for _, id := range ids {
		if h, ok := hashes[id]; ok {
			if out == nil {
				out = Blurhashes{}
			}
			out[id] = h
		}
	}
	return out
}

// photo answers a person's photo, where they have one, and its BlurHash.
func photo(id *uuid.UUID, hash *string) (uuid.UUID, Blurhashes) {
	if id == nil {
		return uuid.UUID{}, nil
	}
	if hash == nil {
		return *id, nil
	}
	return *id, Blurhashes{*id: *hash}
}

// bestPictures answers each title's best picture of each kind, as best_artwork keeps them, and the
// BlurHashes of those that have one: what a card shows, and what a title's page answers, as Plex and
// Jellyfin answer an item with one of each kind and list the rest as candidates.
func (s *Store) bestPictures(ctx context.Context, items []*model.Item) (map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, map[uuid.UUID]string, error) {
	out := map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID{}
	hashes := map[uuid.UUID]string{}
	rows, err := s.pool.Query(ctx, `SELECT item_id, kind, artwork_id, blurhash FROM best_artwork WHERE item_id = ANY($1)`, ids(items))
	if err != nil {
		return nil, nil, err
	}
	var item, id uuid.UUID
	var kind domain.ArtworkKind
	var hash *string
	_, err = pgx.ForEachRow(rows, []any{&item, &kind, &id, &hash}, func() error {
		if out[item] == nil {
			out[item] = map[domain.ArtworkKind][]uuid.UUID{}
		}
		out[item][kind] = []uuid.UUID{id}
		if hash != nil {
			hashes[id] = *hash
		}
		return nil
	})
	return out, hashes, err
}

// BestPicture answers the first of kinds an item has a best picture of, as best_artwork keeps
// them: what an app asks an item for by its id, naming no picture. ErrNotFound where it has none
// of them.
func (s *Store) BestPicture(ctx context.Context, item uuid.UUID, kinds []domain.ArtworkKind) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT artwork_id FROM best_artwork WHERE item_id = $1 AND kind = ANY($2)
		ORDER BY array_position($2, kind) LIMIT 1`, item, kinds).Scan(&id)
	return id, found(err)
}

// keepLibraryPictures chooses again the best pictures of the titles of libs, whose picture sources
// changed.
func keepLibraryPictures(ctx context.Context, tx db, libs []uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT keep_best_artwork(ARRAY(SELECT id FROM items WHERE library_id = ANY($1)))`, libs)
	return err
}

// rankedPictures answers the pictures of items, best first in each title's pictures of a kind, as
// ranked_artwork numbers them.
func (s *Store) rankedPictures(ctx context.Context, items []*model.Item) ([]*model.Artwork, error) {
	if len(items) == 0 {
		return nil, nil
	}
	return queryRows[model.Artwork](ctx, s.pool, `SELECT `+artworkColumns+` FROM ranked_artwork($1)
		ORDER BY item_id, kind, nth`, ids(items))
}

// Unfetched are pictures titles show first that are a provider's, and photos of the people they
// credit (by any source, not only the one shown), that have no BlurHash yet, so have not been
// fetched into the picture cache: their URLs by picture id.
type Unfetched map[uuid.UUID]string

// TitleUnfetched answers the pictures a title, its seasons and their episodes show first, and the
// photos of the people they credit, that have not been fetched.
func (s *Store) TitleUnfetched(ctx context.Context, id uuid.UUID) (Unfetched, error) {
	items, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items
		WHERE id = $1 OR parent_id = $1 OR parent_id IN (SELECT id FROM items WHERE parent_id = $1)`, id)
	if err != nil {
		return nil, err
	}
	return s.unfetched(ctx, items)
}

// Unfetched answers the pictures that up to limit titles, in id order from after, show first, and
// the photos of the people they credit, that have not been fetched, and the last title read, or
// the zero id past the last.
func (s *Store) Unfetched(ctx context.Context, after uuid.UUID, limit int) (Unfetched, uuid.UUID, error) {
	items, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id > $1 ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	out, err := s.unfetched(ctx, items)
	var last uuid.UUID
	if len(items) == limit {
		last = items[len(items)-1].ID
	}
	return out, last, err
}

func (s *Store) unfetched(ctx context.Context, items []*model.Item) (Unfetched, error) {
	rows, err := queryRows[model.Artwork](ctx, s.pool, `SELECT `+artworkColumns+` FROM ranked_artwork($1) WHERE nth = 1`, ids(items))
	if err != nil {
		return nil, err
	}
	out := Unfetched{}
	for _, r := range rows {
		// A file or an upload the server has already.
		if r.Source != domain.SourceFile && r.Source != domain.SourceUpload && r.Blurhash == nil {
			out[r.ID] = r.Place
		}
	}
	photos, err := queryMap[uuid.UUID, string](ctx, s.pool, `
		SELECT DISTINCT p.photo_id, p.photo_url FROM credits c JOIN people p ON p.id = c.person_id
		WHERE c.item_id = ANY($1) AND p.photo_url IS NOT NULL AND p.photo_blurhash IS NULL`, ids(items))
	if err != nil {
		return nil, err
	}
	maps.Copy(out, photos)
	return out, nil
}

// ArtworkCandidate is a picture of a kind a provider has for a title, for an admin to choose, or
// one an admin uploaded.
type ArtworkCandidate struct {
	ID       uuid.UUID
	Source   domain.FieldSource
	Language string
	Width    int
	Height   int
	// Chosen is the one an admin chose.
	Chosen bool
}

// ErrNotACandidate is a picture that is not one a provider has of that kind for the title.
var ErrNotACandidate = errors.New("not a picture a provider has of that kind for the title")

// ArtworkCandidates answers the pictures of a kind each provider had for a title when it was last
// matched, and one an admin uploaded, best first. ErrNotFound for no title.
func (s *Store) ArtworkCandidates(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) ([]ArtworkCandidate, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.rankedPictures(ctx, []*model.Item{item})
	if err != nil {
		return nil, err
	}
	rows = slices.DeleteFunc(rows, func(r *model.Artwork) bool { return r.Kind != kind })
	var chosen string
	if len(rows) > 0 && (rows[0].Source == domain.SourceUser || rows[0].Source == domain.SourceUpload) {
		chosen = rows[0].Place
	}
	var out []ArtworkCandidate
	for _, r := range rows {
		if r.Source == domain.SourceUser || r.Source == domain.SourceFile {
			continue
		}
		out = append(out, ArtworkCandidate{
			ID: r.ID, Source: r.Source, Language: deref(r.Language), Width: deref(r.Width), Height: deref(r.Height),
			Chosen: r.Place == chosen,
		})
	}
	return out, nil
}

// ChooseArtwork makes one of a title's candidates its picture of that kind, above every source,
// as a copy that stands when the provider next says otherwise. ErrNotFound for no title.
func (s *Store) ChooseArtwork(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind, picture uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := forgetChoice(ctx, tx, item.ID, kind); err != nil {
			return err
		}
		pick, err := tx.Exec(ctx, `
			INSERT INTO artwork (item_id, source, kind, place, position, language, width, height, blurhash)
			SELECT item_id, $4, kind, place, 0, language, width, height, blurhash FROM artwork
			WHERE id = $1 AND item_id = $2 AND kind = $3 AND source NOT IN ('file', 'user', 'upload')`,
			picture, item.ID, kind, domain.SourceUser)
		if err == nil && pick.RowsAffected() == 0 {
			err = ErrNotACandidate
		}
		return err
	})
}

// UploadArtwork makes a picture an admin sent, kept under picture, a title's own of its kind,
// above every source and through every refresh, in place of any they chose or sent before.
// ErrNotFound for no title.
func (s *Store) UploadArtwork(ctx context.Context, id, picture uuid.UUID, a domain.Artwork) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := forgetChoice(ctx, tx, item.ID, a.Kind); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO artwork (id, item_id, source, kind, place, position, width, height, blurhash)
			VALUES ($1, $2, $3, $4, $5, 0, $6, $7, $8)`,
			picture, item.ID, domain.SourceUpload, a.Kind, picture.String(), optional(a.Width), optional(a.Height), optional(a.Blurhash))
		return err
	})
}

// ForgetArtworkChoice gives a title's picture of a kind back to its sources, forgetting one an
// admin chose or uploaded. ErrNotFound for no title.
func (s *Store) ForgetArtworkChoice(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) error {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return err
	}
	return forgetChoice(ctx, s.pool, item.ID, kind)
}

func forgetChoice(ctx context.Context, tx db, item uuid.UUID, kind domain.ArtworkKind) error {
	_, err := tx.Exec(ctx, `DELETE FROM artwork WHERE item_id = $1 AND kind = $2 AND source IN ('user', 'upload')`, item, kind)
	return err
}

// Picture answers where a picture is, or ErrNotFound.
func (s *Store) Picture(ctx context.Context, id uuid.UUID) (domain.Picture, error) {
	var source domain.FieldSource
	var place, root string
	err := s.pool.QueryRow(ctx, `
		SELECT a.source, a.place, coalesce(l.root, '') FROM artwork a
		JOIN items i ON i.id = a.item_id LEFT JOIN libraries l ON l.id = i.library_id
		WHERE a.id = $1`, id).Scan(&source, &place, &root)
	if errors.Is(err, pgx.ErrNoRows) {
		// Or a person's.
		var photoURL *string
		err := s.pool.QueryRow(ctx, `SELECT photo_url FROM people WHERE photo_id = $1 LIMIT 1`, id).Scan(&photoURL)
		if err == nil {
			return domain.Picture{URL: deref(photoURL)}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Picture{}, err
		}
		// Or a profile's avatar, kept by the server itself.
		var kept bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE avatar_id = $1)`, id).Scan(&kept); err != nil || kept {
			return domain.Picture{Kept: true}, err
		}
		// Or a video's still.
		var site, key string
		err = s.pool.QueryRow(ctx, `SELECT site, key FROM remote_videos WHERE thumb_id = $1 LIMIT 1`, id).Scan(&site, &key)
		if err == nil {
			return domain.Picture{URL: videoStill(site, key)}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Picture{}, err
		}
		// Or a collection's mosaic, of its titles' posters.
		var parts []uuid.UUID
		err = s.pool.QueryRow(ctx, `SELECT parts FROM mosaics WHERE id = $1`, id).Scan(&parts)
		if err == nil {
			return s.mosaic(ctx, parts)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Picture{}, err
		}
		// Or the poster of a title a search found, or a picture of its page.
		var url *string
		err = s.pool.QueryRow(ctx, `
			SELECT poster_url FROM discoveries WHERE poster_id = $1
			UNION ALL SELECT url FROM discovery_pictures WHERE id = $1 LIMIT 1`, id).Scan(&url)
		return domain.Picture{URL: deref(url)}, found(err)
	}
	if err != nil {
		return domain.Picture{}, err
	}
	// A source is a plugin's as well as a constant's, so this cannot be an exhaustive switch.
	if source == domain.SourceFile {
		return domain.Picture{Root: root, Path: place}, nil
	}
	if source == domain.SourceUpload {
		return domain.Picture{Kept: true}, nil
	}
	return domain.Picture{URL: place}, nil
}

// videoStill is where a video's site publishes a still of it, or "" for a site that publishes none
// without its own API. YouTube's is public and the same for every video.
func videoStill(site, key string) string {
	if strings.EqualFold(site, "youtube") {
		return "https://i.ytimg.com/vi/" + url.PathEscape(key) + "/hqdefault.jpg"
	}
	return ""
}

// LivePictures answers which of these picture ids are still a title's, a person's, a video's, a
// profile's or a collection's mosaic, or a theme tune's kept beside them.
func (s *Store) LivePictures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return querySet[uuid.UUID](ctx, s.pool, `
		SELECT id FROM artwork WHERE id = ANY($1)
		UNION SELECT photo_id FROM people WHERE photo_id = ANY($1)
		UNION SELECT thumb_id FROM remote_videos WHERE thumb_id = ANY($1)
		UNION SELECT avatar_id FROM profiles WHERE avatar_id = ANY($1)
		UNION SELECT id FROM themes WHERE id = ANY($1)
		UNION SELECT id FROM mosaics WHERE id = ANY($1)`, ids)
}

// SetBlurhash keeps the BlurHash of the picture with id, a title's, a person's or a mosaic's.
func (s *Store) SetBlurhash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `
		WITH titles AS (UPDATE artwork SET blurhash = $2 WHERE id = $1),
			mosaics AS (UPDATE mosaics SET blurhash = $2 WHERE id = $1)
		UPDATE people SET photo_blurhash = $2 WHERE photo_id = $1`, id, hash)
	return err
}

// Unhashed is a picture with no BlurHash yet: a file under Root, or, with no Path, one fetched
// into the picture cache under its id, if it has been.
type Unhashed struct {
	ID   uuid.UUID
	Root string
	Path string
}

// Unhashed answers up to limit of the titles' pictures and people's photos with no BlurHash, in
// id order from after.
func (s *Store) Unhashed(ctx context.Context, after uuid.UUID, limit int) ([]Unhashed, error) {
	return queryStructs[Unhashed](ctx, s.pool, `
		(SELECT a.id, CASE a.source WHEN 'file' THEN l.root ELSE '' END AS root,
			CASE a.source WHEN 'file' THEN a.place ELSE '' END AS path
		FROM artwork a JOIN items i ON i.id = a.item_id LEFT JOIN libraries l ON l.id = i.library_id
		WHERE a.blurhash IS NULL AND a.id > @after ORDER BY a.id LIMIT @limit)
		UNION ALL
		(SELECT photo_id, '', '' FROM people WHERE photo_blurhash IS NULL AND photo_id > @after ORDER BY photo_id LIMIT @limit)
		ORDER BY id LIMIT @limit`,
		pgx.NamedArgs{"after": after, "limit": limit})
}
