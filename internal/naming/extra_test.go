package naming

import (
	"testing"
)

func TestExtra(t *testing.T) {
	tests := []struct {
		stem string
		want ExtraKind
	}{
		{"trailer", ExtraTrailer},
		{"Movie (2010)-trailer", ExtraTrailer},
		{"Movie (2010)-trailer2", ExtraTrailer},
		{"Movie (2010).trailer", ExtraTrailer},
		{"Movie (2010) - trailer", ExtraTrailer},
		{"Movie (2010)-sample", ExtraSample},
		{"sample", ExtraSample},
		{"Movie-behindthescenes", ExtraBehindTheScenes},
		{"Movie-deleted", ExtraDeletedScene},
		{"Movie-deletedscene", ExtraDeletedScene},
		{"Movie-featurette", ExtraFeaturette},
		{"Movie-interview", ExtraInterview},
		{"Movie-scene", ExtraScene},
		{"Movie-short", ExtraShort},
		{"Movie-other", ExtraOther},
		// Titles, not extras.
		{"The Short", ""},
		{"Behind the Scene", ""},
		{"Trailer Park Boys", ""},
		{"Movie.featurette", ""},
	}
	for _, tt := range tests {
		t.Run(tt.stem, func(t *testing.T) {
			got, ok := Extra(tt.stem)
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("Extra(%q) = %q, %v; want %q", tt.stem, got, ok, tt.want)
			}
		})
	}
	if k, ok := ExtraFolder("Behind The Scenes"); !ok || k != ExtraBehindTheScenes {
		t.Errorf("ExtraFolder(Behind The Scenes) = %q, %v", k, ok)
	}
	if _, ok := ExtraFolder("Season 1"); ok {
		t.Error("ExtraFolder(Season 1) is an extras folder")
	}
}
