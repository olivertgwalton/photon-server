//go:build integration

package subtitles

import (
	"context"
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// subtitler finds one subtitle for any title, made for the release where a hash is asked by.
type subtitler struct{ asked *[]domain.SubtitleQuery }

func (subtitler) Info() provider.Info {
	return provider.Info{ID: domain.SourceOpenSubtitles, Name: "OpenSubtitles"}
}

func (s subtitler) SearchSubtitles(_ context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	*s.asked = append(*s.asked, q)
	return []domain.FoundSubtitle{{Source: domain.SourceOpenSubtitles, ID: "7", Language: q.Language, Release: "Heat.1995", ForRelease: q.Hash != ""}}, nil
}

func (subtitler) FetchSubtitle(context.Context, string) ([]byte, error) {
	return []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n"), nil
}

func TestASubtitleIsSearchedForByItsFileAndKeptBesideIt(t *testing.T) {
	st, item := heat(t)
	ctx := t.Context()
	var asked []domain.SubtitleQuery
	f := NewFetcher(st, provider.NewRegistry(nil, subtitler{&asked}))
	version, found, err := f.Search(ctx, uuid.UUID{}, item, uuid.UUID{}, language.French)
	if err != nil || len(found) != 1 || !found[0].ForRelease {
		t.Fatalf("Search = %v, %v; want the one made for the file", found, err)
	}
	// OpenSubtitles' hash of 128 KiB of zeros is its size.
	if q := asked[0]; q.Hash != "0000000000020000" || q.Language != language.French || q.IDs[domain.ProviderIMDb] != "tt0113277" {
		t.Errorf("asked %+v; want the film's IMDb id, French and its file's hash", q)
	}
	id, err := f.Fetch(ctx, uuid.UUID{}, item, version, found[0])
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.Playable(ctx, uuid.UUID{}, item, version)
	if err != nil || len(c.Subtitles) != 1 || c.Subtitles[0].ID != id || c.Subtitles[0].Language != language.French {
		t.Errorf("the copy's subtitles: %+v, %v; want the French one fetched", c.Subtitles, err)
	}
}
