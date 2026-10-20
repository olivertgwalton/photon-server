package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// listChunk is how many of a list's items are read and written at a time. An app that sets no
// Limit is answered the whole list, as Jellyfin answers it, so a reply holds no more than this
// many items at once however long the list.
const listChunk = 250

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

// writeList writes a page of cards as a queryResult, listChunk items at a time.
func (a *API) writeList(w http.ResponseWriter, r *http.Request, cards []store.Card, total, start int, l listed) {
	ctx := r.Context()
	items, err := a.list(ctx, cards[:min(listChunk, len(cards))], l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	a.write(w, []byte(`{"Items":[`))
	for n := 0; ; {
		for i := range items {
			// By pointer, an item encodes as it does in a slice: by its fields' pointer methods too.
			body, err := json.Marshal(&items[i])
			if err != nil {
				a.abort(r, err)
			}
			if n > 0 {
				a.write(w, []byte{','})
			}
			a.write(w, body)
			n++
		}
		if n == len(cards) {
			break
		}
		if items, err = a.list(ctx, cards[n:min(n+listChunk, len(cards))], l); err != nil {
			a.abort(r, err)
		}
	}
	a.write(w, fmt.Appendf(nil, `],"TotalRecordCount":%d,"StartIndex":%d}`+"\n", total, start))
}
