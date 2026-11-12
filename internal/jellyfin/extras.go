package jellyfin

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// extraList is which of Jellyfin's lists of a title's extras one is on.
type extraList string

const (
	listTrailers extraList = "trailers"
	listFeatures extraList = "features"
	// listTheme is a theme video's, which Jellyfin lists with neither but as a theme.
	listTheme extraList = "theme"
)

// extraType is Jellyfin's ExtraType of an extra, and the list it is on.
type extraType struct {
	name string
	on   extraList
}

// extraTypes are each kind of extra's. Jellyfin keeps a teaser as a trailer, and has no bloopers.
var extraTypes = map[domain.ExtraKind]extraType{
	domain.ExtraTrailer: {"Trailer", listTrailers}, domain.ExtraTeaser: {"Trailer", listTrailers},
	domain.ExtraFeaturette: {"Featurette", listFeatures}, domain.ExtraBehindTheScenes: {"BehindTheScenes", listFeatures},
	domain.ExtraDeletedScene: {"DeletedScene", listFeatures}, domain.ExtraInterview: {"Interview", listFeatures},
	domain.ExtraScene: {"Scene", listFeatures}, domain.ExtraShort: {"Short", listFeatures},
	domain.ExtraClip: {"Clip", listFeatures}, domain.ExtraBlooper: {"Unknown", listFeatures},
	domain.ExtraOther: {"Unknown", listFeatures}, domain.ExtraThemeVideo: {"ThemeVideo", listTheme},
}

// onLists counts a title's extras on each list.
func onLists(extras []store.ExtraCard) map[extraList]int {
	n := map[extraList]int{}
	for _, e := range extras {
		n[extraTypes[e.Kind].on]++
	}
	return n
}

func (a *API) localTrailers(w http.ResponseWriter, r *http.Request) { a.extras(w, r, listTrailers) }

func (a *API) specialFeatures(w http.ResponseWriter, r *http.Request) { a.extras(w, r, listFeatures) }

// extras answers a title's extras on one list. What is not a title the profile sees has none: Infuse asks of every item it shows, and takes a
// refusal for a failure.
func (a *API) extras(w http.ResponseWriter, r *http.Request, on extraList) {
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	p, err := a.svc.Catalogue.Title(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if isNotFound(err) {
		a.writeJSON(w, []item{})
		return
	}
	var out []item
	if err == nil {
		out, err = a.extraItems(w, r, p.Extras, on)
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, out)
}

// extraItems are those of extras on one list, each an item an app plays as it plays a title.
func (a *API) extraItems(w http.ResponseWriter, r *http.Request, extras []store.ExtraCard, on extraList) ([]item, error) {
	ctx, profile := r.Context(), auth.SessionOf(r.Context()).Profile.ID
	said := words.Negotiate(w, r)
	out := []item{}
	for _, e := range extras {
		kind := extraTypes[e.Kind]
		if kind.on != on {
			continue
		}
		x, err := a.svc.Catalogue.Title(ctx, profile, e.ID)
		if err == nil {
			err = a.chooseTracks(ctx, profile, e.ID, x.Versions)
		}
		if err != nil {
			return nil, err
		}
		it := a.fromTitle(x, said)
		it.ExtraType = kind.name
		it.Etag = etag(it)
		out = append(out, it)
	}
	return out, nil
}
