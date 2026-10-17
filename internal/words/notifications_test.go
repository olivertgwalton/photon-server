package words

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func TestANotificationSaysWhatItBrings(t *testing.T) {
	w := In(language.English)
	one, three := 1, 3
	episode := store.Card{Kind: domain.ItemEpisode, Title: "Half Loop", SeasonNumber: &one, EpisodeNumber: &three}
	for _, c := range []struct {
		new    int
		latest store.Card
		want   string
	}{
		{1, store.Card{Kind: domain.ItemMovie, Title: "Heat"}, "New film"},
		{1, episode, "New episode: S1, E3 · Half Loop"},
		{3, episode, "3 new episodes, the latest S1, E3 · Half Loop"},
		{1, store.Card{Kind: domain.ItemEpisode, Title: "Pilot"}, "New episode: Pilot"},
	} {
		if got := w.Notification(c.new, c.latest); got != c.want {
			t.Errorf("Notification(%d, %+v) = %q, want %q", c.new, c.latest, got, c.want)
		}
	}
}
