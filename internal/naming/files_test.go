package naming

import (
	"testing"
)

func TestFileKinds(t *testing.T) {
	for name, want := range map[string]string{
		"Movie.mkv": "video", "Movie.M2TS": "video", "Movie.iso": "video",
		"Movie.en.srt": "subtitle", "Movie.sup": "subtitle", "Movie.idx": "subtitle",
		"Movie.nfo": "", "poster.jpg": "", "Movie.ogg": "",
	} {
		got := ""
		switch {
		case IsVideo(name):
			got = "video"
		case IsSubtitle(name):
			got = "subtitle"
		}
		if got != want {
			t.Errorf("%s: kind %q, want %q", name, got, want)
		}
	}
}

func TestIgnored(t *testing.T) {
	for name, want := range map[string]bool{
		".DS_Store": true, "._Movie.mkv": true, ".snapshot": true, "@eaDir": true,
		"#recycle": true, "$RECYCLE.BIN": true, "lost+found": true, "Thumbs.db": true,
		"Movie (2010)": false, "Season 1": false, "Extras": false,
	} {
		if got := Ignored(name); got != want {
			t.Errorf("Ignored(%q) = %v, want %v", name, got, want)
		}
	}
}
