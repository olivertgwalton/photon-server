package store

import (
	"cmp"
	"context"
	"errors"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

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
			Language: optional(p.Language), Width: optionalInt(p.Width), Height: optionalInt(p.Height),
		}
	}
	queueArtwork(b, rows)
	return tx.SendBatch(ctx, b).Close()
}

func queueArtwork(b *pgx.Batch, rows []*model.Artwork) {
	for _, r := range rows {
		// A provider may list one picture twice.
		b.Queue(`
			INSERT INTO artwork (item_id, source, kind, place, position, folder, language, width, height, blurhash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT DO NOTHING`,
			r.ItemID, r.Source, r.Kind, r.Place, r.Position, r.Folder, r.Language, r.Width, r.Height, r.Blurhash)
	}
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

// pictureOrder answers each title's pictures by kind, best first, and the BlurHashes of those
// that have one. A picture listed again under a lower source, as one an admin chose is under its
// provider, is left out.
func (s *Store) pictureOrder(ctx context.Context, items []*model.Item) (map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, map[uuid.UUID]string, error) {
	out := map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID{}
	hashes := map[uuid.UUID]string{}
	if len(items) == 0 {
		return out, hashes, nil
	}
	// The pictures and how their libraries rank them are read at once.
	var rows []*model.Artwork
	var taken map[uuid.UUID][]domain.FieldSource
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		rows, err = queryRows[model.Artwork](gctx, s.pool, `SELECT `+artworkColumns+` FROM artwork WHERE item_id = ANY($1)`, ids(items))
		return err
	})
	g.Go(func() (err error) {
		taken, err = rankings(gctx, s.pool, items, domain.FetcherImages)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	rankPictures(rows, items, taken)
	type place struct {
		item  uuid.UUID
		kind  domain.ArtworkKind
		place string
	}
	seen := map[place]bool{}
	for _, r := range rows {
		p := place{r.ItemID, r.Kind, r.Place}
		if seen[p] {
			continue
		}
		seen[p] = true
		if out[r.ItemID] == nil {
			out[r.ItemID] = map[domain.ArtworkKind][]uuid.UUID{}
		}
		out[r.ItemID][r.Kind] = append(out[r.ItemID][r.Kind], r.ID)
		if r.Blurhash != nil {
			hashes[r.ID] = *r.Blurhash
		}
	}
	return out, hashes, nil
}

// rankPictures puts the pictures of items best first: an admin's choice, files beside the title,
// then the providers its library asks for pictures of its kind in the library's order (taken, as
// rankings answers), then each provider's own order.
func rankPictures(rows []*model.Artwork, items []*model.Item, taken map[uuid.UUID][]domain.FieldSource) {
	rank := map[uuid.UUID]map[domain.FieldSource]int{}
	for _, it := range items {
		rank[it.ID] = map[domain.FieldSource]int{domain.SourceUser: -2, domain.SourceFile: -1}
		for n, src := range taken[it.ID] {
			rank[it.ID][src] = n
		}
	}
	// A provider the library no longer asks comes after every one it does.
	order := func(x *model.Artwork) int {
		if r, ok := rank[x.ItemID][x.Source]; ok {
			return r
		}
		return math.MaxInt
	}
	slices.SortStableFunc(rows, func(x, y *model.Artwork) int {
		return cmp.Or(cmp.Compare(order(x), order(y)), cmp.Compare(x.Position, y.Position))
	})
}

// ArtworkCandidate is a picture of a kind a provider has for a title, for an admin to choose.
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
// matched, best first. ErrNotFound for no title.
func (s *Store) ArtworkCandidates(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) ([]ArtworkCandidate, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	rows, err := queryRows[model.Artwork](ctx, s.pool, `SELECT `+artworkColumns+` FROM artwork WHERE item_id = $1 AND kind = $2`, item.ID, kind)
	if err != nil {
		return nil, err
	}
	taken, err := rankings(ctx, s.pool, []*model.Item{item}, domain.FetcherImages)
	if err != nil {
		return nil, err
	}
	rankPictures(rows, []*model.Item{item}, taken)
	var chosen string
	if len(rows) > 0 && rows[0].Source == domain.SourceUser {
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
			WHERE id = $1 AND item_id = $2 AND kind = $3 AND source NOT IN ('file', 'user')`,
			picture, item.ID, kind, domain.SourceUser)
		if err == nil && pick.RowsAffected() == 0 {
			err = ErrNotACandidate
		}
		return err
	})
}

// ForgetArtworkChoice gives a title's picture of a kind back to its sources. ErrNotFound for no
// title.
func (s *Store) ForgetArtworkChoice(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) error {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return err
	}
	return forgetChoice(ctx, s.pool, item.ID, kind)
}

func forgetChoice(ctx context.Context, tx db, item uuid.UUID, kind domain.ArtworkKind) error {
	_, err := tx.Exec(ctx, `DELETE FROM artwork WHERE item_id = $1 AND kind = $2 AND source = $3`, item, kind, domain.SourceUser)
	return err
}

// Picture answers where a picture is, or ErrNotFound.
func (s *Store) Picture(ctx context.Context, id uuid.UUID) (domain.Picture, error) {
	var source domain.FieldSource
	var place, root string
	err := s.pool.QueryRow(ctx, `
		SELECT a.source, a.place, l.root FROM artwork a
		JOIN items i ON i.id = a.item_id JOIN libraries l ON l.id = i.library_id
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
		if err != nil {
			return domain.Picture{}, found(err)
		}
		return domain.Picture{URL: videoStill(site, key)}, nil
	}
	if err != nil {
		return domain.Picture{}, err
	}
	if source != domain.SourceFile {
		return domain.Picture{URL: place}, nil
	}
	return domain.Picture{Root: root, Path: place}, nil
}

// videoStill is where a video's site publishes a still of it, or "" for a site that publishes none
// without its own API. YouTube's is public and the same for every video.
func videoStill(site, key string) string {
	if strings.EqualFold(site, "youtube") {
		return "https://i.ytimg.com/vi/" + url.PathEscape(key) + "/hqdefault.jpg"
	}
	return ""
}

// LivePictures answers which of these picture ids are still a title's, a person's, a video's or a
// profile's, or a theme tune's kept beside them.
func (s *Store) LivePictures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM artwork WHERE id = ANY($1)
		UNION SELECT photo_id FROM people WHERE photo_id = ANY($1)
		UNION SELECT thumb_id FROM remote_videos WHERE thumb_id = ANY($1)
		UNION SELECT avatar_id FROM profiles WHERE avatar_id = ANY($1)
		UNION SELECT id FROM themes WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	var id uuid.UUID
	_, err = pgx.ForEachRow(rows, []any{&id}, func() error {
		out[id] = true
		return nil
	})
	return out, err
}

// SetBlurhash keeps the BlurHash of the picture with id, a title's or a person's.
func (s *Store) SetBlurhash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `
		WITH titles AS (UPDATE artwork SET blurhash = $2 WHERE id = $1)
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
	rows, err := s.pool.Query(ctx, `
		(SELECT a.id, CASE a.source WHEN 'file' THEN l.root ELSE '' END AS root,
			CASE a.source WHEN 'file' THEN a.place ELSE '' END AS path
		FROM artwork a JOIN items i ON i.id = a.item_id JOIN libraries l ON l.id = i.library_id
		WHERE a.blurhash IS NULL AND a.id > @after ORDER BY a.id LIMIT @limit)
		UNION ALL
		(SELECT photo_id, '', '' FROM people WHERE photo_blurhash IS NULL AND photo_id > @after ORDER BY photo_id LIMIT @limit)
		ORDER BY id LIMIT @limit`,
		pgx.NamedArgs{"after": after, "limit": limit})
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Unhashed])
}
