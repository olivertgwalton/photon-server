package store

import (
	"cmp"
	"context"
	"errors"
	"math"
	"path"
	"slices"
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

// pictureOrder answers each title's pictures by kind, best first. A picture listed again under a
// lower source, as one an admin chose is under its provider, is left out.
func (s *Store) pictureOrder(ctx context.Context, items []*model.Item) (map[model.UUID]map[domain.ArtworkKind][]uuid.UUID, error) {
	out := map[model.UUID]map[domain.ArtworkKind][]uuid.UUID{}
	if len(items) == 0 {
		return out, nil
	}
	a := s.q.Artwork
	rows, err := a.WithContext(ctx).Where(a.ItemID.In(ids(items)...)).Find()
	if err != nil {
		return nil, err
	}
	if err := s.rankPictures(ctx, rows, items); err != nil {
		return nil, err
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
	}
	return out, nil
}

// rankPictures puts the pictures of items best first: an admin's choice, files beside the title,
// then its library's providers in the library's order, then each provider's own order.
func (s *Store) rankPictures(ctx context.Context, rows []*model.Artwork, items []*model.Item) error {
	taken, err := s.q.LibrarySource.WithContext(ctx).Find()
	if err != nil {
		return err
	}
	rank := map[model.UUID]map[domain.FieldSource]int{}
	for _, t := range taken {
		if rank[t.LibraryID] == nil {
			rank[t.LibraryID] = map[domain.FieldSource]int{domain.SourceUser: -2, domain.SourceFile: -1}
		}
		rank[t.LibraryID][t.Source] = t.Position
	}
	library := map[model.UUID]model.UUID{}
	for _, it := range items {
		library[it.ID] = it.LibraryID
	}
	// A provider the library has since dropped comes after every one it takes.
	order := func(x *model.Artwork) int {
		if r, ok := rank[library[x.ItemID]][x.Source]; ok {
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
			Language: pick.Language, Width: pick.Width, Height: pick.Height,
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
}

// Picture answers where a picture is, or ErrNotFound.
func (s *Store) Picture(ctx context.Context, id uuid.UUID) (Picture, error) {
	a, i, l := s.q.Artwork, s.q.Item, s.q.Library
	row, err := a.WithContext(ctx).Where(a.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Or a person's.
		p := s.q.Person
		person, err := p.WithContext(ctx).Where(p.PhotoID.Eq(model.UUID(id))).Take()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Picture{}, ErrNotFound
		}
		if err != nil {
			return Picture{}, err
		}
		return Picture{URL: deref(person.PhotoURL)}, nil
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

// LivePictures answers which of these picture ids are still a title's or a person's.
func (s *Store) LivePictures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(ids))
	for start := 0; start < len(ids); start += 1000 {
		batch := ids[start:min(start+1000, len(ids))]
		in := make([]string, len(batch))
		for n, id := range batch {
			in[n] = id.String()
		}
		var found []string
		err := s.q.Artwork.WithContext(ctx).UnderlyingDB().Raw(`
			SELECT id::text FROM artwork WHERE id::text IN ?
			UNION SELECT photo_id::text FROM people WHERE photo_id::text IN ?`, in, in).Scan(&found).Error
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			if id, err := uuid.Parse(f); err == nil {
				out[id] = true
			}
		}
	}
	return out, nil
}
