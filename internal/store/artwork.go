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

	"gorm.io/gorm"

	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// saveFolderArtwork replaces a title's pictures that are files in folder with those found there
// now; pictures it has in other folders stand.
func saveFolderArtwork(ctx context.Context, tx *query.Query, item model.UUID, folder string, pictures []domain.Artwork) error {
	a := tx.Artwork
	_, err := a.WithContext(ctx).Where(a.ItemID.Eq(item), a.Source.Eq(string(domain.SourceFile)), a.Folder.Eq(folder)).Delete()
	if err != nil {
		return err
	}
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
	return createArtwork(ctx, tx, rows)
}

// saveProviderArtwork replaces what a provider has for a title with what it has now.
func saveProviderArtwork(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, pictures []domain.Artwork) error {
	a := tx.Artwork
	if _, err := a.WithContext(ctx).Where(a.ItemID.Eq(item), a.Source.Eq(string(source))).Delete(); err != nil {
		return err
	}
	rows := make([]*model.Artwork, len(pictures))
	for n, p := range pictures {
		rows[n] = &model.Artwork{
			ItemID: item, Source: source, Kind: p.Kind, Place: p.URL, Position: n,
			Language: optional(p.Language), Width: optionalInt(p.Width), Height: optionalInt(p.Height),
		}
	}
	return createArtwork(ctx, tx, rows)
}

func createArtwork(ctx context.Context, tx *query.Query, rows []*model.Artwork) error {
	if len(rows) == 0 {
		return nil
	}
	// A provider may list one picture twice.
	return tx.Artwork.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rows...)
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
func photo(id *model.UUID, hash *string) (uuid.UUID, Blurhashes) {
	if id == nil {
		return uuid.UUID{}, nil
	}
	if hash == nil {
		return uuid.UUID(*id), nil
	}
	return uuid.UUID(*id), Blurhashes{uuid.UUID(*id): *hash}
}

// pictureOrder answers each title's pictures by kind, best first, and the BlurHashes of those
// that have one. A picture listed again under a lower source, as one an admin chose is under its
// provider, is left out.
func (s *Store) pictureOrder(ctx context.Context, items []*model.Item) (map[model.UUID]map[domain.ArtworkKind][]uuid.UUID, map[uuid.UUID]string, error) {
	out := map[model.UUID]map[domain.ArtworkKind][]uuid.UUID{}
	hashes := map[uuid.UUID]string{}
	if len(items) == 0 {
		return out, hashes, nil
	}
	a := s.q.Artwork
	rows, err := a.WithContext(ctx).Where(a.ItemID.In(ids(items)...)).Find()
	if err != nil {
		return nil, nil, err
	}
	if err := s.rankPictures(ctx, rows, items); err != nil {
		return nil, nil, err
	}
	type place struct {
		item  model.UUID
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
		out[r.ItemID][r.Kind] = append(out[r.ItemID][r.Kind], uuid.UUID(r.ID))
		if r.Blurhash != nil {
			hashes[uuid.UUID(r.ID)] = *r.Blurhash
		}
	}
	return out, hashes, nil
}

// rankPictures puts the pictures of items best first: an admin's choice, files beside the title,
// then the providers its library asks for pictures of its kind in the library's order, then each
// provider's own order.
func (s *Store) rankPictures(ctx context.Context, rows []*model.Artwork, items []*model.Item) error {
	taken, err := rankings(ctx, s.q, items, domain.FetcherImages)
	if err != nil {
		return err
	}
	rank := map[model.UUID]map[domain.FieldSource]int{}
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
	return nil
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
	item, err := editable(ctx, s.q, id)
	if err != nil {
		return nil, err
	}
	a := s.q.Artwork
	rows, err := a.WithContext(ctx).Where(a.ItemID.Eq(item.ID), a.Kind.Eq(string(kind))).Find()
	if err != nil {
		return nil, err
	}
	if err := s.rankPictures(ctx, rows, []*model.Item{item}); err != nil {
		return nil, err
	}
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
			ID: uuid.UUID(r.ID), Source: r.Source, Language: deref(r.Language), Width: deref(r.Width), Height: deref(r.Height),
			Chosen: r.Place == chosen,
		})
	}
	return out, nil
}

// ChooseArtwork makes one of a title's candidates its picture of that kind, above every source,
// as a copy that stands when the provider next says otherwise. ErrNotFound for no title.
func (s *Store) ChooseArtwork(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind, picture uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		item, err := editable(ctx, tx, id)
		if err != nil {
			return err
		}
		a := tx.Artwork
		pick, err := a.WithContext(ctx).Where(
			a.ID.Eq(model.UUID(picture)), a.ItemID.Eq(item.ID), a.Kind.Eq(string(kind)),
			a.Source.NotIn(string(domain.SourceFile), string(domain.SourceUser)),
		).Take()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotACandidate
		}
		if err != nil {
			return err
		}
		if err := forgetChoice(ctx, tx, item.ID, kind); err != nil {
			return err
		}
		return a.WithContext(ctx).Create(&model.Artwork{
			ItemID: item.ID, Source: domain.SourceUser, Kind: kind, Place: pick.Place,
			Language: pick.Language, Width: pick.Width, Height: pick.Height, Blurhash: pick.Blurhash,
		})
	})
}

// ForgetArtworkChoice gives a title's picture of a kind back to its sources. ErrNotFound for no
// title.
func (s *Store) ForgetArtworkChoice(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) error {
	item, err := editable(ctx, s.q, id)
	if err != nil {
		return err
	}
	return forgetChoice(ctx, s.q, item.ID, kind)
}

func forgetChoice(ctx context.Context, tx *query.Query, item model.UUID, kind domain.ArtworkKind) error {
	a := tx.Artwork
	_, err := a.WithContext(ctx).Where(a.ItemID.Eq(item), a.Kind.Eq(string(kind)), a.Source.Eq(string(domain.SourceUser))).Delete()
	return err
}

// Picture is where one picture is: a file under Root, or a provider's URL.
type Picture struct {
	Root string
	Path string
	URL  string
	// Kept is a picture given to the server, as an avatar is, held in its picture cache.
	Kept bool
}

// Picture answers where a picture is, or ErrNotFound.
func (s *Store) Picture(ctx context.Context, id uuid.UUID) (Picture, error) {
	a, i, l := s.q.Artwork, s.q.Item, s.q.Library
	row, err := a.WithContext(ctx).Where(a.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Or a person's.
		p := s.q.Person
		person, err := p.WithContext(ctx).Where(p.PhotoID.Eq(model.UUID(id))).Take()
		if err == nil {
			return Picture{URL: deref(person.PhotoURL)}, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return Picture{}, err
		}
		// Or a profile's avatar, kept by the server itself.
		pr := s.q.Profile
		if n, err := pr.WithContext(ctx).Where(pr.AvatarID.Eq(model.UUID(id))).Count(); err != nil || n > 0 {
			return Picture{Kept: true}, err
		}
		// Or a video's still.
		rv := s.q.RemoteVideo
		video, err := rv.WithContext(ctx).Where(rv.ThumbID.Eq(model.UUID(id))).Take()
		if err != nil {
			return Picture{}, found(err)
		}
		return Picture{URL: videoStill(video.Site, video.Key)}, nil
	}
	if err != nil {
		return Picture{}, err
	}
	if row.Source != domain.SourceFile {
		return Picture{URL: row.Place}, nil
	}
	var lib struct{ Root string }
	err = l.WithContext(ctx).Select(l.Root).Join(i, i.LibraryID.EqCol(l.ID)).Where(i.ID.Eq(row.ItemID)).Scan(&lib)
	return Picture{Root: lib.Root, Path: row.Place}, err
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
	in := make([]string, len(ids))
	for n, id := range ids {
		in[n] = id.String()
	}
	live, err := queryIDs(ctx, s.pool, `
		SELECT id::text FROM artwork WHERE id = ANY($1::uuid[])
		UNION SELECT photo_id::text FROM people WHERE photo_id = ANY($1::uuid[])
		UNION SELECT thumb_id::text FROM remote_videos WHERE thumb_id = ANY($1::uuid[])
		UNION SELECT avatar_id::text FROM profiles WHERE avatar_id = ANY($1::uuid[])
		UNION SELECT id::text FROM themes WHERE id = ANY($1::uuid[])`, in)
	out := make(map[uuid.UUID]bool, len(live))
	for _, id := range live {
		out[id] = true
	}
	return out, err
}

// SetBlurhash keeps the BlurHash of the picture with id, a title's or a person's.
func (s *Store) SetBlurhash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `
		WITH titles AS (UPDATE artwork SET blurhash = $2 WHERE id = $1)
		UPDATE people SET photo_blurhash = $2 WHERE photo_id = $1`, id.String(), hash)
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
	var rows []struct {
		ID   model.UUID
		Root string
		Path string
	}
	err := s.q.Artwork.WithContext(ctx).UnderlyingDB().Raw(`
		(SELECT a.id, CASE a.source WHEN 'file' THEN l.root ELSE '' END AS root,
			CASE a.source WHEN 'file' THEN a.place ELSE '' END AS path
		FROM artwork a JOIN items i ON i.id = a.item_id JOIN libraries l ON l.id = i.library_id
		WHERE a.blurhash IS NULL AND a.id > @after ORDER BY a.id LIMIT @limit)
		UNION ALL
		(SELECT photo_id, '', '' FROM people WHERE photo_blurhash IS NULL AND photo_id > @after ORDER BY photo_id LIMIT @limit)
		ORDER BY id LIMIT @limit`,
		map[string]any{"after": model.UUID(after), "limit": limit}).Scan(&rows).Error
	out := make([]Unhashed, len(rows))
	for n, r := range rows {
		out[n] = Unhashed{ID: uuid.UUID(r.ID), Root: r.Root, Path: r.Path}
	}
	return out, err
}
