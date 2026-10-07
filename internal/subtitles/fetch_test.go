//go:build integration

package subtitles

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// subtitler finds one subtitle for any title, made for the release where a hash is asked by and
// the language is French; it answers err to a fetch.
type subtitler struct {
	asked *[]domain.SubtitleQuery
	err   error
}

func (subtitler) Info() provider.Info {
	return provider.Info{ID: domain.SourceOpenSubtitles, Name: "OpenSubtitles"}
}

func (s subtitler) SearchSubtitles(_ context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	*s.asked = append(*s.asked, q)
	return []domain.FoundSubtitle{{Source: domain.SourceOpenSubtitles, ID: "7", Language: q.Language, Release: "Heat.1995", ForRelease: q.Hash != "" && q.Language == language.French}}, nil
}

func (s subtitler) FetchSubtitle(context.Context, string) ([]byte, error) {
	return []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n"), s.err
}

func TestASubtitleIsSearchedForByItsFileAndKeptBesideIt(t *testing.T) {
	st, _, item := heat(t)
	ctx := t.Context()
	var asked []domain.SubtitleQuery
	f := NewFetcher(st, provider.NewRegistry(nil, subtitler{asked: &asked}))
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

func TestALibraryFetchesTheSubtitlesItsCopiesLack(t *testing.T) {
	for _, tc := range []struct {
		match domain.SubtitleMatch
		err   error
		// fetched are the languages kept, and searched how many searches a second run makes.
		fetched  []language.Tag
		searched int
	}{
		// Only French has one made for the file; German's is for another release.
		{match: domain.SubtitleMatchRelease, fetched: []language.Tag{language.French}},
		{match: domain.SubtitleMatchAny, fetched: []language.Tag{language.French, language.German}},
		// Past the quota nothing is kept, and the next run searches again.
		{match: domain.SubtitleMatchAny, err: provider.ErrQuota, searched: 1},
	} {
		t.Run(fmt.Sprint(tc.match, tc.err), func(t *testing.T) {
			st, lib, item := heat(t)
			ctx := t.Context()
			err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{
				SubtitleLanguages: []language.Tag{language.French, language.German}, SubtitleMatch: tc.match,
			})
			if err != nil {
				t.Fatal(err)
			}
			var asked []domain.SubtitleQuery
			f := NewFetcher(st, provider.NewRegistry(nil, subtitler{&asked, tc.err}))
			if n, err := f.FetchMissing(ctx); err != nil || n != len(tc.fetched) {
				t.Fatalf("FetchMissing = %d, %v; want %d", n, err, len(tc.fetched))
			}
			c, err := st.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
			if err != nil {
				t.Fatal(err)
			}
			var kept []language.Tag
			for _, s := range c.Subtitles {
				kept = append(kept, s.Language)
			}
			if !slices.Equal(kept, tc.fetched) {
				t.Errorf("kept %v, want %v", kept, tc.fetched)
			}
			before := len(asked)
			if _, err := f.FetchMissing(ctx); err != nil {
				t.Fatal(err)
			}
			if len(asked)-before != tc.searched {
				t.Errorf("a second run searched %d times, want %d", len(asked)-before, tc.searched)
			}
		})
	}
}
