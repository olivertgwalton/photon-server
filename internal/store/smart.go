package store

import (
	"cmp"
	"errors"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// SmartRule is what a smart collection's titles are, as Plex's smart collections: the films and
// shows of the libraries it reads as a wall's filter narrows them, in a wall's order, at most Limit of them, 0 for
// all. Its titles are the same for everyone, so it reads no profile's marks or plays, and each
// viewer sees what they may of them.
type SmartRule struct {
	Filter WallFilter      `json:"filter"`
	Sort   domain.WallSort `json:"sort,omitzero"`
	Order  domain.Order    `json:"order,omitzero"`
	Limit  int             `json:"limit,omitzero"`
}

// ErrRuleForSomeone is a rule that would read one profile's marks or plays.
var ErrRuleForSomeone = errors.New("a smart collection is everyone's: its rule reads no one's marks or plays")

// Check is whether a rule finds the same titles for everyone, with values a wall takes.
func (r SmartRule) Check() error {
	if len(r.Filter.Marks) > 0 || r.Sort == domain.SortPlayed {
		return ErrRuleForSomeone
	}
	if r.Limit < 0 {
		return errors.New("limit is 0, for all, or more")
	}
	return r.Filter.Check()
}

// page is the wall a rule reads, in title order where it names none.
func (r SmartRule) page() WallPage {
	sort := cmp.Or(r.Sort, domain.SortTitle)
	return WallPage{
		Sort: sort, Order: cmp.Or(r.Order, sort.DefaultOrder()),
		RatingSite: cmp.Or(r.Filter.RatingSite, domain.SiteIMDb), Filter: r.Filter,
	}
}
