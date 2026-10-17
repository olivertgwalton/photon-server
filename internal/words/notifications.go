package words

import (
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Notification says what a notification brings, beside its title's name: "New film", "New episode:
// S1, E3 · Half Loop", or "3 new episodes, the latest S1, E5 · The You You Are". The comma
// is how the web app and the Photon apps place an episode, and this is read beside them.
func (w Words) Notification(count int, latest store.Card) string {
	if latest.Kind != domain.ItemEpisode {
		return "New film"
	}
	var named []string
	if latest.EpisodeNumber != nil {
		at := "E" + strconv.Itoa(*latest.EpisodeNumber)
		if latest.SeasonNumber != nil {
			at = "S" + strconv.Itoa(*latest.SeasonNumber) + ", " + at
		}
		named = append(named, at)
	}
	if latest.Title != "" {
		named = append(named, latest.Title)
	}
	episode := strings.Join(named, " · ")
	if count == 1 {
		return "New episode: " + episode
	}
	return strconv.Itoa(count) + " new episodes, the latest " + episode
}
