package naming

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var imageExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".tbn"}

func IsImage(name string) bool { return hasExt(imageExtensions, name) }

// artworkNames are Jellyfin's and Kodi's names for a folder's own pictures.
var artworkNames = map[string]domain.ArtworkKind{
	"poster": domain.ArtworkPoster, "folder": domain.ArtworkPoster, "cover": domain.ArtworkPoster,
	"default": domain.ArtworkPoster, "movie": domain.ArtworkPoster, "show": domain.ArtworkPoster,
	"fanart": domain.ArtworkBackdrop, "backdrop": domain.ArtworkBackdrop,
	"background": domain.ArtworkBackdrop, "art": domain.ArtworkBackdrop,
	"logo": domain.ArtworkLogo, "clearlogo": domain.ArtworkLogo,
	"thumb": domain.ArtworkThumb, "landscape": domain.ArtworkThumb,
	"banner": domain.ArtworkBanner,
}

// backdropNumber is a backdrop's number after its name: "backdrop2", "fanart-3".
var backdropNumber = regexp.MustCompile(`-?\d+$`)

// Artwork reads an image's name as Jellyfin does. A folder's own picture is named for its kind
// ("poster.jpg", "fanart.jpg", "backdrop2.jpg"), and answers an empty stem. A picture of one
// file's title is that file's stem with its kind ("Heat (1995)-poster.jpg"), or the stem alone
// ("Heat (1995).jpg"), the title's main picture, which answers no kind.
func Artwork(name string) (stem string, kind domain.ArtworkKind, ok bool) {
	if !IsImage(name) {
		return "", "", false
	}
	base := strings.TrimSuffix(name, path.Ext(name))
	if k, ok := folderArtwork(base); ok {
		return "", k, true
	}
	if i := strings.LastIndexByte(base, '-'); i > 0 {
		if k, ok := folderArtwork(base[i+1:]); ok {
			return base[:i], k, true
		}
	}
	return base, "", true
}

func folderArtwork(name string) (domain.ArtworkKind, bool) {
	name = strings.ToLower(name)
	if k, ok := artworkNames[name]; ok {
		return k, true
	}
	if k, ok := artworkNames[backdropNumber.ReplaceAllString(name, "")]; ok && k == domain.ArtworkBackdrop {
		return k, true
	}
	return "", false
}

var seasonArtwork = regexp.MustCompile(`(?i)^season(\d+|-specials)-([a-z]+)$`)

// SeasonArtwork reads a season's picture kept in its series' folder: "season01-poster.jpg",
// "season-specials-fanart.jpg".
func SeasonArtwork(name string) (season int, kind domain.ArtworkKind, ok bool) {
	if !IsImage(name) {
		return 0, "", false
	}
	m := seasonArtwork.FindStringSubmatch(strings.TrimSuffix(name, path.Ext(name)))
	if m == nil {
		return 0, "", false
	}
	if kind, ok = folderArtwork(m[2]); !ok {
		return 0, "", false
	}
	if !strings.EqualFold(m[1], "-specials") {
		season, _ = strconv.Atoi(m[1])
	}
	return season, kind, true
}
