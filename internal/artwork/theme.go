package artwork

import (
	"context"
	"fmt"
	"path"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// tunes are the theme tunes fetched from ThemerrDB's links, as a Cache keeps them.
type tunes interface {
	Kept(ctx context.Context, id uuid.UUID) (blob.Object, error)
}

// OpenTheme opens theme tune id where it came from: rel under a library's root, or fetched and
// kept in kept. It answers the tune's type, "" where it is not known: the standard library knows
// no sound file's type by its name.
func OpenTheme(ctx context.Context, kept tunes, id uuid.UUID, source domain.ThemeSource, root, rel string) (blob.Object, string, error) {
	switch source {
	case domain.ThemeFromFile:
		f, err := library.Open(root, rel)
		if err != nil {
			return blob.Object{}, "", err
		}
		o, err := blob.OfFile(f)
		kind, _ := naming.AudioType(path.Base(rel))
		return o, kind, err
	case domain.ThemeFromThemerr:
		o, err := kept.Kept(ctx, id)
		return o, "audio/mp4", err
	}
	return blob.Object{}, "", fmt.Errorf("theme from %q", source)
}
