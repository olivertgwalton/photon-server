package naming

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestExtra(t *testing.T) {
	tests := []struct {
		stem  string
		kind  domain.ExtraKind
		owner string
	}{
		{"trailer", domain.ExtraTrailer, ""},
		{"Movie (2010)-trailer", domain.ExtraTrailer, "Movie (2010)"},
		{"Movie (2010)-trailer2", domain.ExtraTrailer, "Movie (2010)"},
		{"Movie (2010).trailer", domain.ExtraTrailer, "Movie (2010)"},
		{"Movie (2010) - trailer", domain.ExtraTrailer, "Movie (2010)"},
		{"Movie-behindthescenes", domain.ExtraBehindTheScenes, "Movie"},
		{"Movie-deleted", domain.ExtraDeletedScene, "Movie"},
		{"Movie-deletedscene", domain.ExtraDeletedScene, "Movie"},
		{"Movie-featurette", domain.ExtraFeaturette, "Movie"},
		{"Movie-interview", domain.ExtraInterview, "Movie"},
		{"Movie-scene", domain.ExtraScene, "Movie"},
		{"Movie-short", domain.ExtraShort, "Movie"},
		{"Movie-other", domain.ExtraOther, "Movie"},
		{"The Wire S01E01-deleted", domain.ExtraDeletedScene, "The Wire S01E01"},
		// Titles, not extras.
		{"The Short", "", ""},
		{"Behind the Scene", "", ""},
		{"Trailer Park Boys", "", ""},
		{"Movie.featurette", "", ""},
		{"Movie-sample", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.stem, func(t *testing.T) {
			kind, owner, ok := Extra(tt.stem)
			if kind != tt.kind || owner != tt.owner || ok != (tt.kind != "") {
				t.Errorf("Extra(%q) = %q, %q, %v; want %q, %q", tt.stem, kind, owner, ok, tt.kind, tt.owner)
			}
		})
	}
	if k, ok := ExtraFolder("Behind The Scenes"); !ok || k != domain.ExtraBehindTheScenes {
		t.Errorf("ExtraFolder(Behind The Scenes) = %q, %v", k, ok)
	}
	for _, name := range []string{"Season 1", "Samples"} {
		if _, ok := ExtraFolder(name); ok {
			t.Errorf("ExtraFolder(%q) is an extras folder", name)
		}
	}
}

func TestSample(t *testing.T) {
	for stem, want := range map[string]bool{
		"sample": true, "Movie-sample": true, "Movie.sample": true, "Movie sample2": true,
		"Sampled": false, "The Sampler": false, "Free Samples": false,
	} {
		if got := Sample(stem); got != want {
			t.Errorf("Sample(%q) = %v, want %v", stem, got, want)
		}
	}
	if !SampleFolder("Samples") || SampleFolder("Season 1") {
		t.Error("SampleFolder misread")
	}
}
