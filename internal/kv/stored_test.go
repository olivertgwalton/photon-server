package kv

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A playback another server kept reads back as it was kept, its times in nanoseconds, and is kept
// the same way again.
func TestAPlaybackKeptElsewhereReadsBack(t *testing.T) {
	const record = `{"Position":90000000000,"Length":7200000000000}`
	var p domain.Playback
	if err := json.Unmarshal([]byte(record), &p, stored); err != nil || p.Position != 90*time.Second || p.Length != 2*time.Hour {
		t.Fatalf("read %s as %v of %v (err %v); want 1m30s of 2h", record, p.Position, p.Length, err)
	}
	b, err := json.Marshal(p, stored)
	var again domain.Playback
	if err == nil {
		err = json.Unmarshal(b, &again, stored)
	}
	if err != nil || again.Position != p.Position || again.Length != p.Length {
		t.Errorf("kept again as %s, read back as %v of %v (err %v)", b, again.Position, again.Length, err)
	}
}
