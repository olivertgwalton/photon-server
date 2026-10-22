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

// Every enum column's CHECK constraint, named after its Go type, allows exactly the Go constants.
func TestEnumConstraintsMatchGo(t *testing.T) {
	s := migrated(t)
	for constraint, want := range map[string][]string{
		"library_kind":             names(domain.LibraryKinds()),
		"plugin_protocol":          names(domain.PluginProtocols()),
		"library_media":            names(domain.LibraryMedias()),
		"discovery_kind":           names([]domain.ItemKind{domain.ItemMovie, domain.ItemShow}),
		"monitor":                  names(domain.Monitors()),
		"preview_level":            names(domain.PreviewLevels()),
		"marker_detection":         names(domain.MarkerDetections()),
		"keyframe_mode":            names(domain.KeyframeModes()),
		"media_deletion":           names(domain.MediaDeletions()),
		"missing_version":          names(domain.MissingVersions()),
		"artwork_language":         names(domain.ArtworkLanguages()),
		"title_language":           names(domain.TitleLanguages()),
		"box_set_mode":             names(domain.BoxSetModes()),
		"request_state":            names(domain.RequestStates()),
		"request_kind":             names([]domain.ItemKind{domain.ItemMovie, domain.ItemShow}),
		"collection_box_set":       names(domain.CollectionBoxSets()),
		"subtitle_match":           names(domain.SubtitleMatches()),
		"theme_lookup":             names(domain.ThemeLookups()),
		"theme_source":             names(domain.ThemeSources()),
		"theme_music":              names(domain.ThemeMusics()),
		"card_art":                 names(domain.CardArts()),
		"episode_spoilers":         names(domain.SpoilerModes()),
		"notify":                   names(domain.Notifies()),
		"push_kind":                names(domain.PushKinds()),
		"apns_environment":         names(domain.APNsEnvironments()),
		"artwork_kind":             names(domain.ArtworkKinds()),
		"item_kind":                names(domain.ItemKinds()),
		"id_source":                names(domain.IDSources()),
		"stream_kind":              names(domain.StreamKinds()),
		"video_range":              names(domain.Ranges()),
		"stream_video_range":       names(domain.Ranges()),
		"job_kind":                 names(domain.JobKinds()),
		"webhook_format":           names(domain.WebhookFormats()),
		"job_due":                  names(domain.JobDues()),
		"job_state":                names(domain.JobStates()),
		"profile_role":             names(domain.Roles()),
		"secure_connections":       names(domain.SecureConnectionModes()),
		"session_kind":             names(domain.SessionKinds()),
		"extra_kind":               names(domain.ExtraKinds()),
		"field":                    names(domain.Fields()),
		"library_source_item_kind": names(append(domain.LibraryMovies.ItemKinds(), domain.LibraryShows.ItemKinds()...)),
		"library_source_fetcher":   names(domain.Fetchers()),
		"library_extra_kind":       names(domain.ExtraKinds()),
		"remote_video_kind":        names(domain.ExtraKinds()),
		"task_key":                 names(domain.TaskKeys()),
		"task_result":              names(domain.TaskResults()),
		"rating_site":              names(domain.RatingSites()),
		"collection_origin":        names(domain.CollectionOrigins()),
		"credit_kind":              names(domain.CreditKinds()),
		"profile_unrated":          names(domain.UnratedPolicies()),
		"provisioning":             names(domain.Provisionings()),
		"sign_in_provider_unrated": names(domain.UnratedPolicies()),
		"recheck":                  names(domain.Rechecks()),
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
		"previews_timing":          names(domain.Timings()),
		"markers_timing":           names(domain.Timings()),
		"delivery_event":           names(append(domain.HookableEventKinds(), domain.EventWebhookTest)),
		"import_source":            names(domain.ImportSources()),
		"import_status":            names(domain.ImportStatuses()),
		"tracker":                  names(domain.Trackers()),
		"account_tracker":          names(domain.Trackers()),
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

// A source or provider a plugin may fill is checked in the database for its shape alone, so that
// adding one changes no check: every Go constant, and a plugin's name, has that shape.
func TestProviderConstraintsTakeEveryGoName(t *testing.T) {
	s := migrated(t)
	for constraint, want := range map[string][]string{
		"field_source":        names(domain.FieldSources()),
		"library_source":      names(domain.MetadataSources()),
		"artwork_source":      names(domain.ArtworkSources()),
		"rating_source":       names(domain.RatingSources()),
		"credit_source":       names(domain.CreditSources()),
		"id_provider":         names(domain.Providers()),
		"person_id_provider":  names(domain.Providers()),
		"discovery_provider":  names(domain.Providers()),
		"request_provider":    names(domain.Providers()),
		"remote_video_source": {string(domain.SourceTMDB), string(domain.SourceTVDB)},
		"announced_source":    {string(domain.SourceTMDB), string(domain.SourceTVDB)},
	} {
		var def string
		err := s.pool.QueryRow(t.Context(),
			"SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1", constraint).Scan(&def)
		if err != nil {
			t.Fatalf("%s: %v", constraint, err)
		}
		patterns := quoted.FindAllStringSubmatch(def, -1)
		if len(patterns) != 1 {
			t.Errorf("%s: %s, want one pattern", constraint, def)
			continue
		}
		shape := regexp.MustCompile(patterns[0][1])
		for _, name := range append(want, "plugin:my-provider") {
			if !shape.MatchString(name) {
				t.Errorf("%s: the database refuses %q", constraint, name)
			}
		}
		for _, junk := range []string{"", "TMDB", "plugin:", "tmdb;"} {
			if shape.MatchString(junk) {
				t.Errorf("%s: the database takes %q", constraint, junk)
			}
		}
	}
}
