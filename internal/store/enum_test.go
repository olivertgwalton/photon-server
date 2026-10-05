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
		"library_kind":       names(domain.LibraryKinds()),
		"monitor":            names(domain.Monitors()),
		"artwork_source":     names(domain.ArtworkSources()),
		"artwork_kind":       names(domain.ArtworkKinds()),
		"item_kind":          names(domain.ItemKinds()),
		"id_provider":        names(domain.Providers()),
		"id_source":          names(domain.IDSources()),
		"stream_kind":        names(domain.StreamKinds()),
		"video_range":        names(domain.Ranges()),
		"stream_video_range": names(domain.Ranges()),
		"job_kind":           names(domain.JobKinds()),
		"job_state":          names(domain.JobStates()),
		"profile_role":       names(domain.Roles()),
		"extra_kind":         names(domain.ExtraKinds()),
		"field":              names(domain.Fields()),
		"field_source":       names(domain.FieldSources()),
		"library_source":     names(domain.MetadataSources()),
		"library_extra_kind": names(domain.ExtraKinds()),
		"remote_video_kind":  names(domain.ExtraKinds()),
		"task_key":           names(domain.TaskKeys()),
		"task_result":        names(domain.TaskResults()),
		"rating_source":      names(domain.RatingSources()),
		"rating_site":        names(domain.RatingSites()),
		"collection_origin":  names(domain.CollectionOrigins()),
		"credit_source":      names(domain.CreditSources()),
		"credit_kind":        names(domain.CreditKinds()),
		"profile_unrated":    names(domain.UnratedPolicies()),
		"play_method":        names(domain.PlayMethods()),
		"episode_order":      names(domain.EpisodeOrders()),
		"marker_kind":        names(domain.MarkerKinds()),
		"marker_source":      names(domain.MarkerSources()),
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
