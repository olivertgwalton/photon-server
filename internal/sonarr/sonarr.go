// Package sonarr asks Sonarr to fetch the shows a library lacks that its profiles ask for, as
// Overseerr does: the show is added to Sonarr with the seasons asked for monitored and searched
// for, and Sonarr writes their episodes into the library's folder, where a scan finds them.
package sonarr

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/arr"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// Client asks the Sonarr at the address an admin sets.
type Client struct {
	settings provider.Settings
}

func New(settings provider.Settings) *Client {
	return &Client{settings: settings}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceSonarr, Name: "Sonarr", Kinds: []domain.ItemKind{domain.ItemShow},
		Settings: arr.Settings("Sonarr", "http://sonarr:8989"),
	}
}

type season struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

// found is a show as Sonarr's lookup answers it: with an id where Sonarr has it already.
type found struct {
	ID         int             `json:"id"`
	Title      string          `json:"title"`
	TitleSlug  string          `json:"titleSlug"`
	Year       int             `json:"year"`
	SeriesType string          `json:"seriesType"`
	Images     json.RawMessage `json:"images"`
	Seasons    []season        `json:"seasons"`
}

type series struct {
	Title            string          `json:"title"`
	TitleSlug        string          `json:"titleSlug"`
	TVDBID           int             `json:"tvdbId"`
	Year             int             `json:"year,omitempty"`
	SeriesType       string          `json:"seriesType"`
	Images           json.RawMessage `json:"images,omitempty"`
	Seasons          []season        `json:"seasons"`
	QualityProfileID int             `json:"qualityProfileId"`
	RootFolderPath   string          `json:"rootFolderPath"`
	SeasonFolder     bool            `json:"seasonFolder"`
	Monitored        bool            `json:"monitored"`
	AddOptions       addOptions      `json:"addOptions"`
}

type addOptions struct {
	SearchForMissingEpisodes bool `json:"searchForMissingEpisodes"`
}

// Request adds a show to Sonarr by its TheTVDB id, with the seasons asked for monitored and searched
// for at once, every season but its specials where none are named, with the quality profile and
// root folder an admin named, or Sonarr's first of each. A show Sonarr has already is taken on
// where it monitors those seasons, and refused where it does not, as Sonarr's own monitoring is
// the admin's to change there.
func (c *Client) Request(ctx context.Context, w domain.Wanted) error {
	if w.Kind != domain.ItemShow {
		return fmt.Errorf("sonarr fetches shows, not a %s", w.Kind)
	}
	tvdb, err := strconv.Atoi(w.IDs[domain.ProviderTVDB])
	if err != nil {
		return errors.New("sonarr knows a show by its TheTVDB id, and this one has none")
	}
	api, err := arr.New(ctx, "sonarr", c.settings)
	if err != nil {
		return err
	}
	var shows []found
	if err := api.Get(ctx, "/api/v3/series/lookup", url.Values{"term": {"tvdb:" + strconv.Itoa(tvdb)}}, &shows); err != nil {
		return err
	}
	if len(shows) == 0 {
		return fmt.Errorf("sonarr finds no show with TheTVDB id %d", tvdb)
	}
	show := shows[0]
	asked := func(n int) bool { return slices.Contains(w.Seasons, n) || len(w.Seasons) == 0 && n > 0 }
	if show.ID != 0 {
		for _, s := range show.Seasons {
			if asked(s.SeasonNumber) && !s.Monitored {
				return fmt.Errorf("sonarr has the show already, and does not monitor season %d: monitor it in Sonarr", s.SeasonNumber)
			}
		}
		return nil
	}
	seasons := make([]season, len(show.Seasons))
	monitored := false
	for i, s := range show.Seasons {
		seasons[i] = season{SeasonNumber: s.SeasonNumber, Monitored: asked(s.SeasonNumber)}
		monitored = monitored || seasons[i].Monitored
	}
	if !monitored {
		return fmt.Errorf("sonarr knows none of seasons %v of the show", w.Seasons)
	}
	profile, folder, err := api.Place(ctx)
	if err != nil {
		return err
	}
	return api.Post(ctx, "/api/v3/series", series{
		Title: cmp.Or(show.Title, w.Title), TitleSlug: show.TitleSlug, TVDBID: tvdb, Year: cmp.Or(show.Year, w.Year),
		SeriesType: cmp.Or(show.SeriesType, "standard"), Images: show.Images, Seasons: seasons,
		QualityProfileID: profile, RootFolderPath: folder, SeasonFolder: true, Monitored: true,
		AddOptions: addOptions{SearchForMissingEpisodes: true},
	})
}
