package store

import (
	"cmp"
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// SmartRule is what a smart collection's titles, or a smart playlist's entries, are, as Plex's
// smart collections and playlists: what the libraries it reads hold of the kind it names, as a
// wall's filter narrows them, in a wall's order, at most Limit of them, 0 for all. Its JSON, and
// its Filter's, is how a rule is kept in the rule column, not what the API writes.
type SmartRule struct {
	// Kind is what the rule finds, a wall's titles where it names none.
	Kind   domain.WallKind `json:"kind,omitzero"`
	Filter WallFilter      `json:"filter"`
	Sort   domain.WallSort `json:"sort,omitzero"`
	Order  domain.Order    `json:"order,omitzero"`
	Limit  int             `json:"limit,omitzero"`
}

// ErrRuleForSomeone is a rule that would read one profile's marks or plays.
var ErrRuleForSomeone = errors.New("a smart collection is everyone's: its rule reads no one's marks or plays")

// ErrRuleNotTitles is a smart collection's rule naming anything but the titles it holds.
var ErrRuleNotTitles = errors.New("a smart collection holds titles: its rule names kind titles, or none")

// ErrRuleNotPlayable is a smart playlist's rule naming anything it could not play.
var ErrRuleNotPlayable = errors.New("a smart playlist plays what it finds: its rule names kind films or episodes")

// Check is whether a rule has values a wall takes.
func (r SmartRule) Check() error {
	if r.Limit < 0 {
		return errors.New("limit is 0, for all, or more")
	}
	return r.Filter.Check()
}

// CheckShared is whether a rule is one a smart collection may keep: its titles are the same for
// everyone, so it reads no profile's marks or plays, and each viewer sees what they may of them.
func (r SmartRule) CheckShared() error {
	if len(r.Filter.Marks) > 0 || r.Sort == domain.SortPlayed {
		return ErrRuleForSomeone
	}
	if r.Kind != "" && r.Kind != domain.WallTitles {
		return ErrRuleNotTitles
	}
	return r.Check()
}

// CheckPlayable is whether a rule is one a smart playlist may keep: a playlist plays its entries
// one after another, so a rule finds films or episodes, never the shows they are of.
func (r SmartRule) CheckPlayable() error {
	if r.Kind != domain.WallFilms && r.Kind != domain.WallEpisodes {
		return ErrRuleNotPlayable
	}
	return r.Check()
}

// page is the wall a rule reads, in title order where it names none.
func (r SmartRule) page() WallPage {
	sort := cmp.Or(r.Sort, domain.SortTitle)
	return WallPage{
		Kind: r.Kind,
		Sort: sort, Order: cmp.Or(r.Order, sort.DefaultOrder()),
		RatingSite: cmp.Or(r.Filter.RatingSite, domain.SiteIMDb), Filter: r.Filter,
		// A rule finds titles, never the box sets they are in.
		BoxSets: domain.BoxSetsExpanded,
	}
}

// playlistPage is the page of the wall a smart playlist's rule reads for the profile whose it is:
// its own marks and plays narrow and sort it, and a rule that takes only its first findings stops
// a page at them.
func (r SmartRule) playlistPage(profile uuid.UUID, offset, limit int) WallPage {
	p := r.page()
	p.Profile, p.Offset, p.Limit = profile, offset, limit
	if r.Limit > 0 {
		p.Limit = max(0, min(limit, r.Limit-offset))
	}
	return p
}

// AddSmartCollection makes an admin's collection whose titles a rule finds in libs.
func (s *Store) AddSmartCollection(ctx context.Context, libs []uuid.UUID, title string, rule SmartRule) (uuid.UUID, error) {
	if err := rule.CheckShared(); err != nil {
		return uuid.UUID{}, err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibraries(ctx, tx, libs); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, title, madeBy{origin: domain.CollectionSmart, rule: &rule, libraries: libs}); err != nil {
			return err
		}
		if err := applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title}); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, id, libs, rule)
	})
	return id, err
}

// SetRule replaces a smart collection's rule, and its titles with what it finds.
func (s *Store) SetRule(ctx context.Context, collection uuid.UUID, rule SmartRule) error {
	if err := rule.CheckShared(); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		libs, err := userCollection(ctx, tx, collection, domain.CollectionSmart)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE collections SET rule = $2 WHERE item_id = $1`, collection, rule); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, collection, libs, rule)
	})
}

// findMembers replaces a smart collection's titles with what its rule finds now, in its order.
func (s *Store) findMembers(ctx context.Context, tx db, collection uuid.UUID, libs []uuid.UUID, rule SmartRule) error {
	where, args, err := s.wallQuery(ctx, libs, uuid.UUID{}, domain.WallTitles, rule.Filter)
	if err != nil {
		return err
	}
	join, order := rule.page().orderBy(args)
	args["collection"] = collection
	limit := ""
	if rule.Limit > 0 {
		args["limit"], limit = rule.Limit, ` LIMIT @limit`
	}
	if _, err := tx.Exec(ctx, `DELETE FROM collection_members WHERE collection_id = @collection`, args); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO collection_members (collection_id, item_id, position)
		SELECT @collection, items.id, row_number() OVER (`+order+`) - 1 FROM items`+join+` `+where+` `+order+limit, args)
	return err
}

// RefreshSmartCollections finds the titles of the smart collections reading any of libs again, or
// of every one where none is named.
func (s *Store) RefreshSmartCollections(ctx context.Context, libs ...uuid.UUID) error {
	type smart struct {
		ItemID    uuid.UUID
		Libraries []uuid.UUID
		Rule      SmartRule
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, libraries, rule FROM collections
		WHERE origin = 'smart' AND ($1::uuid[] IS NULL OR libraries && $1)`, libs)
	if err != nil {
		return err
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[smart])
	if err != nil {
		return err
	}
	for _, c := range all {
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			return s.findMembers(ctx, tx, c.ItemID, c.Libraries, c.Rule)
		}); err != nil {
			return err
		}
	}
	return nil
}
