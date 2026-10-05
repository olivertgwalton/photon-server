package naming

import (
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var extraFolders = map[string]domain.ExtraKind{
	"trailers":          domain.ExtraTrailer,
	"featurettes":       domain.ExtraFeaturette,
	"behind the scenes": domain.ExtraBehindTheScenes,
	"deleted scenes":    domain.ExtraDeletedScene,
	"interviews":        domain.ExtraInterview,
	"scenes":            domain.ExtraScene,
	"shorts":            domain.ExtraShort,
	"clips":             domain.ExtraClip,
	"backdrops":         domain.ExtraThemeVideo,
	"extras":            domain.ExtraOther,
	"extra":             domain.ExtraOther,
	"other":             domain.ExtraOther,
}

// ExtraFolder reports whether a folder holds extras rather than titles.
func ExtraFolder(name string) (domain.ExtraKind, bool) {
	k, ok := extraFolders[strings.ToLower(name)]
	return k, ok
}

// SampleFolder reports whether a folder holds samples, which are never kept.
func SampleFolder(name string) bool {
	n := strings.ToLower(name)
	return n == "sample" || n == "samples"
}

var extraSuffixes = []struct {
	suffix string
	kind   domain.ExtraKind
}{
	{"trailer", domain.ExtraTrailer},
	{"featurette", domain.ExtraFeaturette},
	{"behindthescenes", domain.ExtraBehindTheScenes},
	{"deletedscene", domain.ExtraDeletedScene},
	{"deleted", domain.ExtraDeletedScene},
	{"interview", domain.ExtraInterview},
	{"scene", domain.ExtraScene},
	{"short", domain.ExtraShort},
	{"clip", domain.ExtraClip},
	{"extra", domain.ExtraOther},
	{"other", domain.ExtraOther},
}

// Extra reads a file's stem: "trailer", "Movie (2010)-trailer2". A trailer may follow any
// separator; the rest need a hyphen, as in Jellyfin and Plex, so a title ending in "Scene" or
// "Short" is not an extra. owner is what the stem names before the suffix, empty for a bare
// "trailer".
func Extra(stem string) (kind domain.ExtraKind, owner string, ok bool) {
	trimmed := strings.TrimRight(stem, "0123456789")
	s := strings.ToLower(trimmed)
	for _, e := range extraSuffixes {
		if s == e.suffix && e.kind == domain.ExtraTrailer {
			return e.kind, "", true
		}
		before, found := strings.CutSuffix(s, e.suffix)
		if !found || strings.TrimRight(before, " ") == "" {
			continue
		}
		last := strings.TrimRight(before, " ")
		sep := last[len(last)-1]
		if sep == '-' || (e.kind == domain.ExtraTrailer && strings.ContainsRune("._", rune(sep))) {
			return e.kind, strings.TrimRight(trimmed[:len(last)-1], " ._-"), true
		}
	}
	return "", "", false
}

// Sample reports whether a file is a sample, by name: "sample", "Movie-sample", "Movie.sample".
// Never part of a word, so "Sampled" is not one.
func Sample(stem string) bool {
	s := strings.ToLower(strings.TrimRight(stem, "0123456789"))
	if s == "sample" {
		return true
	}
	before, found := strings.CutSuffix(s, "sample")
	return found && before != "" && strings.ContainsRune(" .-_", rune(before[len(before)-1]))
}
