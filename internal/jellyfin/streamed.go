package jellyfin

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// placeholder is the one media source a remote film or episode with no copy yet is listed with,
// under the title's own id, as Remux lists one: an app takes an item with no media source to be
// unplayable (Infuse fails the page it is on), and its copy is fetched only as it is played.
func placeholder(id uuid.UUID, title string) mediaSource {
	return mediaSource{
		Protocol: "File", ID: guid(id), Path: title, Type: "Default", Name: title, ETag: guid(id),
		SupportsDirectStream: true, SupportsDirectPlay: true, SupportsTranscoding: true, VideoType: "VideoFile",
		MediaStreams: []mediaStream{}, MediaAttachments: []struct{}{}, Formats: []string{}, TranscodingSubProtocol: "http",
	}
}

// withPlaceholders gives each of items with no copy that is a remote film or episode its
// placeholder, by the titles' ids and names.
func (a *API) withPlaceholders(ctx context.Context, items []item, ids []uuid.UUID, names []string) error {
	streamed, err := a.svc.Catalogue.Streamed(ctx, ids)
	if err != nil {
		return err
	}
	for n, id := range ids {
		if streamed[id] && len(items[n].MediaSources) == 0 {
			items[n].MediaSources = []mediaSource{placeholder(id, names[n])}
			items[n].Path = names[n]
		}
	}
	return nil
}

// listPlaceholders gives a list's remote films and episodes with no copy yet their placeholders,
// where an app asks for media sources.
func (a *API) listPlaceholders(ctx context.Context, items []item, cards []store.Card, l listed) error {
	if !l.fields["mediasources"] {
		return nil
	}
	ids, names := make([]uuid.UUID, len(cards)), make([]string, len(cards))
	for n, c := range cards {
		ids[n], names[n] = c.ID, c.Title
	}
	return a.withPlaceholders(ctx, items, ids, names)
}

// titlePlaceholder is a title's own page, with its placeholder where it is a remote film or
// episode with no copy yet.
func (a *API) titlePlaceholder(ctx context.Context, it item, id uuid.UUID, title string) (item, error) {
	single := []item{it}
	err := a.withPlaceholders(ctx, single, []uuid.UUID{id}, []string{title})
	return single[0], err
}

// copyAsked is the copy an app asks for of a title: a placeholder's id, the title's own, is the one
// photon would play.
func copyAsked(title, version uuid.UUID) uuid.UUID {
	if version == title {
		return uuid.UUID{}
	}
	return version
}
