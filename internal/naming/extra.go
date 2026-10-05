package naming

import (
	"strings"
)

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
	ExtraSample          ExtraKind = "sample"
	ExtraThemeVideo      ExtraKind = "theme_video"
	ExtraOther           ExtraKind = "other"
)

var extraFolders = map[string]ExtraKind{
	"trailers":          ExtraTrailer,
	"featurettes":       ExtraFeaturette,
	"behind the scenes": ExtraBehindTheScenes,
	"deleted scenes":    ExtraDeletedScene,
	"interviews":        ExtraInterview,
	"scenes":            ExtraScene,
	"shorts":            ExtraShort,
	"clips":             ExtraClip,
	"samples":           ExtraSample,
	"sample":            ExtraSample,
	"backdrops":         ExtraThemeVideo,
	"extras":            ExtraOther,
	"extra":             ExtraOther,
	"other":             ExtraOther,
}

// ExtraFolder reports whether a folder holds extras rather than titles.
func ExtraFolder(name string) (ExtraKind, bool) {
	k, ok := extraFolders[strings.ToLower(name)]
	return k, ok
}

var extraSuffixes = []struct {
	suffix string
	kind   ExtraKind
}{
	{"trailer", ExtraTrailer},
	{"sample", ExtraSample},
	{"featurette", ExtraFeaturette},
	{"behindthescenes", ExtraBehindTheScenes},
	{"deletedscene", ExtraDeletedScene},
	{"deleted", ExtraDeletedScene},
	{"interview", ExtraInterview},
	{"scene", ExtraScene},
	{"short", ExtraShort},
	{"clip", ExtraClip},
	{"extra", ExtraOther},
	{"other", ExtraOther},
}

// Extra reads a file's stem: "trailer", "Movie (2010)-trailer2", "Movie.sample". Trailer and sample
// may follow any separator; the rest need a hyphen, as in Jellyfin and Plex, so a title ending in
// "Scene" or "Short" is not an extra.
func Extra(stem string) (ExtraKind, bool) {
	s := strings.ToLower(strings.TrimRight(stem, "0123456789"))
	for _, e := range extraSuffixes {
		if s == e.suffix && (e.kind == ExtraTrailer || e.kind == ExtraSample) {
			return e.kind, true
		}
		before, found := strings.CutSuffix(s, e.suffix)
		if !found || before == "" {
			continue
		}
		before = strings.TrimRight(before, " ")
		last := before[len(before)-1]
		if last == '-' || ((e.kind == ExtraTrailer || e.kind == ExtraSample) && strings.ContainsRune("._", rune(last))) {
			return e.kind, true
		}
	}
	return "", false
}
