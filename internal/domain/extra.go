package domain

// ExtraKind is what an extra is: Jellyfin's and Plex's kinds together. A sample is not an extra;
// it is never kept.
type ExtraKind string

const (
	ExtraTrailer         ExtraKind = "trailer"
	ExtraFeaturette      ExtraKind = "featurette"
	ExtraBehindTheScenes ExtraKind = "behind_the_scenes"
	ExtraDeletedScene    ExtraKind = "deleted_scene"
	ExtraInterview       ExtraKind = "interview"
	ExtraScene           ExtraKind = "scene"
	ExtraShort           ExtraKind = "short"
	ExtraClip            ExtraKind = "clip"
	ExtraThemeVideo      ExtraKind = "theme_video"
	ExtraOther           ExtraKind = "other"
)

func ExtraKinds() []ExtraKind {
	return []ExtraKind{
		ExtraTrailer, ExtraFeaturette, ExtraBehindTheScenes, ExtraDeletedScene, ExtraInterview,
		ExtraScene, ExtraShort, ExtraClip, ExtraThemeVideo, ExtraOther,
	}
}
