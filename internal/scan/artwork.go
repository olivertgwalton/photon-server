package scan

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// pictures is a folder's images sorted by whose they are: the folder's own title's, each file's
// by its stem (a stem's main picture has no kind), and, in a series' folder, each season's.
type pictures struct {
	own     []domain.Artwork
	byStem  map[string][]domain.Artwork
	seasons map[int][]domain.Artwork
}

// picturesIn sorts the pictures among names in dir, each with its BlurHash, as Jellyfin takes one
// as it saves a picture.
func picturesIn(root, dir string, names []string) pictures {
	p := pictures{byStem: map[string][]domain.Artwork{}, seasons: map[int][]domain.Artwork{}}
	for _, name := range names {
		rel := path.Join(dir, name)
		if season, kind, ok := naming.SeasonArtwork(name); ok {
			p.seasons[season] = append(p.seasons[season], picture(root, rel, kind))
			continue
		}
		stem, kind, ok := naming.Artwork(name)
		switch {
		case !ok:
		case stem == "":
			p.own = append(p.own, picture(root, rel, kind))
		default:
			key := strings.ToLower(stem)
			p.byStem[key] = append(p.byStem[key], picture(root, rel, kind))
		}
	}
	return p
}

// picture is the file at rel, with its BlurHash where it is a picture decoded here.
func picture(root, rel string, kind domain.ArtworkKind) domain.Artwork {
	hash, _ := artwork.FileBlurhash(root, rel)
	return domain.Artwork{Kind: kind, Path: rel, Blurhash: hash}
}

// of answers the pictures named after a file's stem, its main picture as main.
func (p pictures) of(stem string, main domain.ArtworkKind) []domain.Artwork {
	var out []domain.Artwork
	for _, a := range p.byStem[strings.ToLower(stem)] {
		if a.Kind == "" {
			a.Kind = main
		}
		out = append(out, a)
	}
	return out
}

// seasonPictures answers the pictures a series' folder keeps for one season.
func seasonPictures(root, series string, season int) []domain.Artwork {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(series)))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n, _, ok := naming.SeasonArtwork(e.Name()); ok && n == season {
			names = append(names, e.Name())
		}
	}
	return picturesIn(root, series, names).seasons[season]
}
