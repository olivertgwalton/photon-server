//go:build integration

package analysis

import (
	"cmp"
	"os"
	"os/exec"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestAnalysingAFileSavesWhatItHolds(t *testing.T) {
	path, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFPROBE"), "ffprobe"))
	if err != nil {
		t.Skipf("needs ffprobe: %v", err)
	}
	f := newFixture(t)
	// The film is saved as read to run 1050 s in 640×360; its file is the media package's Matroska
	// fixture, which runs a few seconds.
	_, part := f.film(media9(t, indexedFile))
	if err := Probe(f.st, media.Tools{FFprobe: media.Tool{Path: path}})(t.Context(), part); err != nil {
		t.Fatal(err)
	}
	var ms int64
	var width int
	if err := f.db.QueryRow(t.Context(), `
		SELECT p.duration_ms, s.width FROM parts p JOIN streams s ON s.part_id = p.id AND s.kind = 'video'
		WHERE p.id = $1`, part.String()).Scan(&ms, &width); err != nil {
		t.Fatal(err)
	}
	if ms <= 0 || ms >= 1_050_000 || width == 640 {
		t.Errorf("after reading again: %d ms, %d wide; want the file's own length and picture", ms, width)
	}
	if err := Probe(f.st, media.Tools{FFprobe: media.Tool{Path: path}})(t.Context(), uuid.NewV7()); err != nil {
		t.Errorf("a part gone since: %v, want it passed over", err)
	}
}
