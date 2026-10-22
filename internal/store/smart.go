package store

import (
	"cmp"
	"errors"
	"uuid"

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
