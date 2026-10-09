package jellyfin

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// searchHint is Jellyfin's SearchHint, as much of it as a title has. ItemId is Id under the name
// older apps read it by.
type searchHint struct {
	ItemID                  string   `json:"ItemId"`
	ID                      string   `json:"Id"`
	Name                    string   `json:"Name"`
	IndexNumber             *int     `json:"IndexNumber,omitempty"`
	ParentIndexNumber       *int     `json:"ParentIndexNumber,omitempty"`
	ProductionYear          int      `json:"ProductionYear,omitempty"`
	PrimaryImageTag         string   `json:"PrimaryImageTag,omitempty"`
	ThumbImageTag           string   `json:"ThumbImageTag,omitempty"`
	ThumbImageItemID        string   `json:"ThumbImageItemId,omitempty"`
	BackdropImageTag        string   `json:"BackdropImageTag,omitempty"`
	BackdropImageItemID     string   `json:"BackdropImageItemId,omitempty"`
	Type                    string   `json:"Type"`
	IsFolder                bool     `json:"IsFolder"`
	RunTimeTicks            int64    `json:"RunTimeTicks,omitempty"`
	MediaType               string   `json:"MediaType"`
	Series                  string   `json:"Series,omitempty"`
	Artists                 []string `json:"Artists"`
	PrimaryImageAspectRatio float64  `json:"PrimaryImageAspectRatio,omitempty"`
}

// hintOf is an item as a search hint: an episode's backdrop is its show's.
func hintOf(it item) searchHint {
	h := searchHint{
		ItemID: it.ID, ID: it.ID, Name: it.Name, IndexNumber: it.IndexNumber, ParentIndexNumber: it.ParentIndexNumber,
		ProductionYear: it.ProductionYear, PrimaryImageTag: it.ImageTags["Primary"], Type: it.Type, IsFolder: it.IsFolder,
		RunTimeTicks: it.RunTimeTicks, MediaType: it.MediaType, Series: it.SeriesName, Artists: []string{},
		PrimaryImageAspectRatio: it.PrimaryImageAspectRatio,
	}
	if tag, ok := it.ImageTags["Thumb"]; ok {
		h.ThumbImageTag, h.ThumbImageItemID = tag, it.ID
	}
	switch {
	case len(it.BackdropImageTags) > 0:
		h.BackdropImageTag, h.BackdropImageItemID = it.BackdropImageTags[0], it.ID
	case len(it.ParentBackdropImageTags) > 0:
		h.BackdropImageTag, h.BackdropImageItemID = it.ParentBackdropImageTags[0], it.ParentBackdropItemID
	}
	return h
}

// searchHints answers Jellyfin's /Search/Hints: the titles matching what was typed, as /Items
// finds them. Only titles are hinted, not people, whom photon searches for apart.
func (a *API) searchHints(w http.ResponseWriter, r *http.Request) {
	text, l := query(r, "searchTerm"), listedOf(w, r)
	parent, ok := optionalID(query(r, "parentId"))
	if !ok || text == "" {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	type result struct {
		SearchHints      []searchHint `json:"SearchHints"`
		TotalRecordCount int          `json:"TotalRecordCount"`
	}
	out := result{SearchHints: []searchHint{}}
	q, ok := searchQuery(r, text, parent, values(r, "includeItemTypes"), l)
	if !ok {
		a.writeJSON(w, out)
		return
	}
	cards, total, err := a.searched(r.Context(), q)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for _, c := range cards {
		out.SearchHints = append(out.SearchHints, hintOf(a.fromCard(c)))
	}
	out.TotalRecordCount = total
	a.writeJSON(w, out)
}

// searched is what a search of every library finds: its titles, and, first in its first page, the
// titles its remote libraries' providers find that they do not hold yet, which become theirs as
// an app opens one.
func (a *API) searched(ctx context.Context, q store.SearchQuery) ([]store.Card, int, error) {
	cards, total, err := a.svc.Catalogue.Search(ctx, q)
	if err != nil || q.Offset > 0 || q.Library != (uuid.UUID{}) {
		return cards, int(total), err
	}
	found, err := a.svc.Discover.Find(ctx, q.Profile, q.Text, q.Kinds)
	if err != nil {
		return nil, 0, err
	}
	for _, d := range found {
		cards = append(cards, d.Card())
	}
	return cards, int(total) + len(found), nil
}
