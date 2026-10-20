package jellyfin

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// list writes cards as items, with their copies, whether each downloads, trickplay, chapters and
// providers' ids where the app asked for them: read for the whole page at once.
func (a *API) list(ctx context.Context, cards []store.Card, l listed) ([]item, error) {
	out := make([]item, len(cards))
	ids := make([]uuid.UUID, len(cards))
	for n, c := range cards {
		out[n], ids[n] = a.fromCard(c), c.ID
	}
	if l.fields["mediasources"] || l.fields["mediastreams"] || l.fields["candownload"] || l.fields["trickplay"] ||
		l.fields["chapters"] {
		versions, err := a.svc.Catalogue.Versions(ctx, ids)
		if err != nil {
			return nil, err
		}
		for n, id := range ids {
			v := versions[id]
			out[n].CanDownload = downloadable(v)
			if l.fields["trickplay"] {
				out[n].trickplay(v)
			}
			if l.fields["chapters"] {
				out[n].chapters(v)
			}
			if len(v) > 0 && (l.fields["mediasources"] || l.fields["mediastreams"]) {
				out[n].sources(v, l.words)
				// Jellyfin writes the first copy's tracks beside it only when asked: they double
				// what is written of the item.
				if l.fields["mediastreams"] {
					out[n].MediaStreams = out[n].MediaSources[0].MediaStreams
				}
			}
		}
		if err := a.listPlaceholders(ctx, out, cards, l); err != nil {
			return nil, err
		}
	}
	if l.fields["providerids"] {
		external, err := a.svc.Catalogue.ExternalIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for n, id := range ids {
			out[n].ProviderIDs = providerIDs(external[id])
		}
	}
	for n := range out {
		out[n].Etag = etag(out[n])
	}
	return out, nil
}

func (a *API) writeList(w http.ResponseWriter, r *http.Request, cards []store.Card, total, start int, l listed) {
	items, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, queryResult{Items: items, TotalRecordCount: total, StartIndex: start})
}
