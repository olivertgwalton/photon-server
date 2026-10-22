package httpapi

import (
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// smartRuleJSON is what finds a smart collection's titles or a smart playlist's entries. The store
// keeps a rule as JSON of its own, so this is written apart from it: a name changed on the wire
// moves no rule already kept.
type smartRuleJSON struct {
	// Kind is what the rule finds, a wall's titles where it names none.
	Kind   domain.WallKind `json:"kind,omitzero"`
	Filter wallFilterJSON  `json:"filter"`
	Sort   domain.WallSort `json:"sort,omitzero"`
	Order  domain.Order    `json:"order,omitzero"`
	Limit  int             `json:"limit,omitzero"`
}

type wallFilterJSON struct {
	// StartsWith is a letter its sort title starts with, unaccented, or "#" for before A.
	StartsWith   string              `json:"starts_with,omitzero"`
	Marks        []domain.Mark       `json:"marks,omitzero"`
	Genres       []string            `json:"genres,omitzero"`
	Years        []int               `json:"years,omitzero"`
	Certificates []string            `json:"certificates,omitzero"`
	Studios      []string            `json:"studios,omitzero"`
	Resolutions  []domain.Resolution `json:"resolutions,omitzero"`
	Ranges       []domain.Range      `json:"ranges,omitzero"`
	// People are credited on it, or on one of its episodes.
	People []uuid.UUID `json:"people,omitzero"`
	// MinRating is the least RatingSite's score out of 100 may be, zero for any.
	RatingSite domain.RatingSite `json:"rating_site,omitzero"`
	MinRating  float64           `json:"min_rating,omitzero"`
}

func (j smartRuleJSON) rule() store.SmartRule {
	return store.SmartRule{Kind: j.Kind, Filter: store.WallFilter(j.Filter), Sort: j.Sort, Order: j.Order, Limit: j.Limit}
}

func ruleOf(r *store.SmartRule) *smartRuleJSON {
	if r == nil {
		return nil
	}
	return &smartRuleJSON{Kind: r.Kind, Filter: wallFilterJSON(r.Filter), Sort: r.Sort, Order: r.Order, Limit: r.Limit}
}

// listRefJSON is the list kept on a provider a list collection or a remote library holds, and how
// many of its titles the libraries it reads lack.
type listRefJSON struct {
	Source  domain.FieldSource `json:"source"`
	ID      string             `json:"id"`
	Missing int                `json:"missing,omitzero"`
}

func (j listRefJSON) ref() store.ListRef { return store.ListRef(j) }

func listOf(l *store.ListRef) *listRefJSON {
	if l == nil {
		return nil
	}
	j := listRefJSON(*l)
	return &j
}
