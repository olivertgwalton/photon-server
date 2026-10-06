package domain

import (
	"slices"
	"strings"
	"time"
)

// ExtraKind is what an extra is: Jellyfin's and Plex's kinds together. A sample is not an extra;
// it is never kept.
type ExtraKind string

const (
	ExtraTrailer         ExtraKind = "trailer"
	ExtraTeaser          ExtraKind = "teaser"
	ExtraFeaturette      ExtraKind = "featurette"
	ExtraBehindTheScenes ExtraKind = "behind_the_scenes"
	ExtraDeletedScene    ExtraKind = "deleted_scene"
	ExtraInterview       ExtraKind = "interview"
	ExtraScene           ExtraKind = "scene"
	ExtraShort           ExtraKind = "short"
	ExtraClip            ExtraKind = "clip"
	ExtraBlooper         ExtraKind = "blooper"
	ExtraThemeVideo      ExtraKind = "theme_video"
	ExtraOther           ExtraKind = "other"
)

func ExtraKinds() []ExtraKind {
	return []ExtraKind{
		ExtraTrailer, ExtraTeaser, ExtraFeaturette, ExtraBehindTheScenes, ExtraDeletedScene,
		ExtraInterview, ExtraScene, ExtraShort, ExtraClip, ExtraBlooper, ExtraThemeVideo, ExtraOther,
	}
}

// DefaultRemoteExtras are the kinds a library fetches from providers unless told otherwise.
func DefaultRemoteExtras() []ExtraKind {
	return []ExtraKind{ExtraTrailer, ExtraFeaturette, ExtraBehindTheScenes}
}

func ParseExtraKinds(list string) ([]ExtraKind, error) {
	if list == "none" {
		return []ExtraKind{}, nil
	}
	var out []ExtraKind
	for name := range strings.SplitSeq(list, ",") {
		k, err := Parse("extra kind", strings.TrimSpace(name), ExtraKinds())
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

// RemoteVideo is a video a provider links to rather than one in the library: a trailer on YouTube.
type RemoteVideo struct {
	Kind      ExtraKind
	Site      string
	Key       string
	Name      string
	Language  string
	Published time.Time
}
