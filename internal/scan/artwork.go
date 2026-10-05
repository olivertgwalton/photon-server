package scan

import (
	"io/fs"
	"os"
	"path"
	"strings"

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

func picturesIn(dir string, names []string) pictures {
	p := pictures{byStem: map[string][]domain.Artwork{}, seasons: map[int][]domain.Artwork{}}
	for _, name := range names {
		rel := path.Join(dir, name)
		if season, kind, ok := naming.SeasonArtwork(name); ok {
			p.seasons[season] = append(p.seasons[season], domain.Artwork{Kind: kind, Path: rel})
			continue
		}
		stem, kind, ok := naming.Artwork(name)
		switch {
		case !ok:
		case stem == "":
			p.own = append(p.own, domain.Artwork{Kind: kind, Path: rel})
		default:
			key := strings.ToLower(stem)
			p.byStem[key] = append(p.byStem[key], domain.Artwork{Kind: kind, Path: rel})
		}
	}
	return p
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
func seasonPictures(root *os.Root, series string, season int) []domain.Artwork {
	entries, err := fs.ReadDir(root.FS(), series)
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return picturesIn(series, names).seasons[season]
}
