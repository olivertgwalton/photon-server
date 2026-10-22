package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// listChunk is how many of a list's items are drawn and written at a time. An app that sets no
// Limit is answered the whole list, as Jellyfin answers it, so a reply holds no more than this
// many items at once however long the list. Fewer, and a library's worth of chunks costs more
// round trips to the database than the reply saves.
const listChunk = 2500

// list writes cards as items, with their versions, whether each downloads, trickplay, chapters and
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
			v := onDisk(versions[id])
			out[n].CanDownload = downloadable(v)
			if l.fields["trickplay"] {
				out[n].trickplay(v)
			}
			if l.fields["chapters"] {
				out[n].chapters(v)
			}
			if len(v) > 0 && (l.fields["mediasources"] || l.fields["mediastreams"]) {
				out[n].sources(v, l.words)
				// Jellyfin writes the first version's tracks beside it only when asked: they double
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
	a.writeChunks(w, r, len(cards), total, start, func(ctx context.Context, from, to int) ([]item, error) {
		return a.list(ctx, cards[from:to], l)
	})
}

// writeTitles writes the titles ids names as a queryResult, drawing listChunk of them at a time. A
// title gone before its chunk is drawn is left out.
func (a *API) writeTitles(w http.ResponseWriter, r *http.Request, ids []uuid.UUID, total, start int, l listed) {
	profile := auth.SessionOf(r.Context()).Profile.ID
	a.writeChunks(w, r, len(ids), total, start, func(ctx context.Context, from, to int) ([]item, error) {
		cards, err := a.svc.Catalogue.Cards(ctx, profile, ids[from:to])
		if err != nil {
			return nil, err
		}
		return a.list(ctx, cards, l)
	})
}

// writeChunks writes the items chunk answers for n places as a queryResult, listChunk at a time.
func (a *API) writeChunks(w http.ResponseWriter, r *http.Request, n, total, start int, chunk func(ctx context.Context, from, to int) ([]item, error)) {
	ctx := r.Context()
	read := func(from int) ([]item, error) { return chunk(ctx, from, min(from+listChunk, n)) }
	items, err := read(0)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	a.write(w, []byte(`{"Items":[`))
	written := 0
	for from := 0; ; {
		for i := range items {
			// By pointer, an item encodes as it does in a slice: by its fields' pointer methods too.
			body, err := json.Marshal(&items[i])
			if err != nil {
				a.abort(r, err)
			}
			if written > 0 {
				a.write(w, []byte{','})
			}
			a.write(w, body)
			written++
		}
		if from += listChunk; from >= n {
			break
		}
		if items, err = read(from); err != nil {
			a.abort(r, err)
		}
	}
	a.write(w, fmt.Appendf(nil, `],"TotalRecordCount":%d,"StartIndex":%d}`+"\n", total, start))
}
