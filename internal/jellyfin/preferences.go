package jellyfin

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type preferences interface {
	Preferences(ctx context.Context, profile uuid.UUID) (domain.Preferences, error)
	ChosenTracks(ctx context.Context, profile, item uuid.UUID) (domain.ChosenTracks, error)
}

// chooseTracks says the tracks each copy of a title plays with for a profile, by the preferences
// and choices photon's own apps play by, as Jellyfin's media sources answer their defaults.
func (a *API) chooseTracks(ctx context.Context, profile, item uuid.UUID, versions []store.VersionPage) error {
	if len(versions) == 0 {
		return nil
	}
	prefs, err := a.svc.Preferences.Preferences(ctx, profile)
	if err != nil {
		return err
	}
	last, err := a.svc.Preferences.ChosenTracks(ctx, profile, item)
	if err != nil {
		return err
	}
	playback.ChooseTracks(versions, prefs, last)
	return nil
}
