//go:build integration

package store

import (
	"regexp"
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func names[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

var quoted = regexp.MustCompile(`'([^']*)'`)

// Every enum column's CHECK constraint, named after its Go type, allows exactly the Go constants,
// and a source or id kind a plugin may have exactly the plugin pattern beside them.
func TestEnumConstraintsMatchGo(t *testing.T) {
	s := migrated(t)
	plugins := func(values []string) []string { return append(values, domain.PluginPattern) }
	for constraint, want := range map[string][]string{
		"library_kind":             names(domain.LibraryKinds()),
		"monitor":                  names(domain.Monitors()),
		"preview_level":            names(domain.PreviewLevels()),
		"marker_detection":         names(domain.MarkerDetections()),
		"keyframe_mode":            names(domain.KeyframeModes()),
		"theme_lookup":             names(domain.ThemeLookups()),
		"theme_source":             names(domain.ThemeSources()),
		"theme_music":              names(domain.ThemeMusics()),
		"artwork_source":           plugins(names(domain.ArtworkSources())),
		"artwork_kind":             names(domain.ArtworkKinds()),
		"item_kind":                names(domain.ItemKinds()),
		"id_provider":              plugins(names(domain.Providers())),
		"person_id_provider":       plugins(names(domain.Providers())),
		"id_source":                names(domain.IDSources()),
		"stream_kind":              names(domain.StreamKinds()),
		"video_range":              names(domain.Ranges()),
		"stream_video_range":       names(domain.Ranges()),
		"job_kind":                 names(domain.JobKinds()),
		"job_state":                names(domain.JobStates()),
		"profile_role":             names(domain.Roles()),
		"extra_kind":               names(domain.ExtraKinds()),
		"field":                    names(domain.Fields()),
		"field_source":             plugins(names(domain.FieldSources())),
		"library_source":           plugins(names(domain.MetadataSources())),
		"library_source_item_kind": names(append(domain.LibraryMovies.ItemKinds(), domain.LibraryShows.ItemKinds()...)),
		"library_source_fetcher":   names(domain.Fetchers()),
		"library_extra_kind":       names(domain.ExtraKinds()),
		"remote_video_kind":        names(domain.ExtraKinds()),
		"remote_video_source":      plugins([]string{string(domain.SourceTMDB), string(domain.SourceTVDB)}),
		"task_key":                 names(domain.TaskKeys()),
		"task_result":              names(domain.TaskResults()),
		"rating_source":            plugins(names(domain.RatingSources())),
		"rating_site":              names(domain.RatingSites()),
		"collection_origin":        names(domain.CollectionOrigins()),
		"credit_source":            plugins(names(domain.CreditSources())),
		"credit_kind":              names(domain.CreditKinds()),
		"profile_unrated":          names(domain.UnratedPolicies()),
		"audio_track":              names(domain.AudioTracks()),
		"subtitle_mode":            names(domain.SubtitleModes()),
		"remember_audio":           names(domain.TrackMemories()),
		"remember_subtitles":       names(domain.TrackMemories()),
		"next_episode":             names(domain.NextEpisodes()),
		"intro_action":             names(domain.SegmentActions()),
		"credits_action":           names(domain.SegmentActions()),
		"home_row":                 names(domain.HomeRows()),
		"row_visibility":           names(domain.RowVisibilities()),
		"play_method":              names(domain.PlayMethods()),
		"episode_order":            names(domain.EpisodeOrders()),
		"marker_kind":              names(domain.MarkerKinds()),
		"marker_source":            names(domain.MarkerSources()),
		"download_state":           names(domain.DownloadStates()),
		"video_codec":              names(domain.VideoCodecs()),
		"conversion_video_range":   names(domain.Ranges()),
		"activity_kind":            names(domain.LoggedEventKinds()),
		"webhook_event":            names(domain.HookableEventKinds()),
		"delivery_event":           names(append(domain.HookableEventKinds(), domain.EventWebhookTest)),
	} {
		var def string
		err := s.pool.QueryRow(t.Context(),
			"SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1", constraint).Scan(&def)
		if err != nil {
			t.Fatalf("%s: %v", constraint, err)
		}
		var got []string
		for _, m := range quoted.FindAllStringSubmatch(def, -1) {
			got = append(got, m[1])
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: database allows %v, Go has %v", constraint, got, want)
		}
	}
}
