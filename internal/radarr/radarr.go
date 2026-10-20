// Package radarr asks Radarr to fetch the films a library lacks that its profiles ask for, as
// Overseerr does: the film is added to Radarr, monitored and searched for, and Radarr writes it
// into the library's folder, where a scan finds it.
package radarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	addressSetting = "address"
	keySetting     = "api_key"
	profileSetting = "quality_profile"
	folderSetting  = "root_folder"
)

// Client asks the Radarr at the address an admin sets.
type Client struct {
	settings provider.Settings
}

func New(settings provider.Settings) *Client {
	return &Client{settings: settings}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceRadarr, Name: "Radarr", Kinds: []domain.ItemKind{domain.ItemMovie},
		Settings: []provider.Setting{
			{Key: addressSetting, Name: "Address, such as http://radarr:7878", Required: true},
			{Key: keySetting, Name: "API key", Secret: true, Required: true},
			{Key: profileSetting, Name: "Quality profile, by name; Radarr's first where unset"},
			{Key: folderSetting, Name: "Root folder, by path; Radarr's first where unset"},
		},
	}
}

type qualityProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type rootFolder struct {
	Path string `json:"path"`
}

type movie struct {
	Title               string     `json:"title"`
	TMDBID              int        `json:"tmdbId"`
	Year                int        `json:"year,omitempty"`
	QualityProfileID    int        `json:"qualityProfileId"`
	RootFolderPath      string     `json:"rootFolderPath"`
	Monitored           bool       `json:"monitored"`
	MinimumAvailability string     `json:"minimumAvailability"`
	AddOptions          addOptions `json:"addOptions"`
}

type addOptions struct {
	SearchForMovie bool `json:"searchForMovie"`
}

// Request adds a film to Radarr by its TMDB id, monitored and searched for at once, with the
// quality profile and root folder an admin named, or Radarr's first of each. A film Radarr has
// already is taken on as it is.
func (c *Client) Request(ctx context.Context, w domain.Wanted) error {
	if w.Kind != domain.ItemMovie {
		return fmt.Errorf("radarr fetches films, not a %s", w.Kind)
	}
	tmdb, err := strconv.Atoi(w.IDs[domain.ProviderTMDB])
	if err != nil {
		return errors.New("radarr knows a film by its TMDB id, and this one has none")
	}
	set, err := c.settings(ctx)
	if err != nil {
		return err
	}
	if set[addressSetting] == "" || set[keySetting] == "" {
		return provider.ErrNotConfigured
	}
	api := radarr{provider.Client{Name: "radarr", Base: set[addressSetting]}, set[keySetting]}
	var had []json.RawMessage
	if err := api.get(ctx, "/api/v3/movie", url.Values{"tmdbId": {strconv.Itoa(tmdb)}}, &had); err != nil || len(had) > 0 {
		return err
	}
	var profiles []qualityProfile
	if err := api.get(ctx, "/api/v3/qualityprofile", nil, &profiles); err != nil {
		return err
	}
	profile, ok := named(profiles, set[profileSetting], func(p qualityProfile) string { return p.Name })
	if !ok {
		return fmt.Errorf("radarr has no quality profile named %q", set[profileSetting])
	}
	var folders []rootFolder
	if err := api.get(ctx, "/api/v3/rootfolder", nil, &folders); err != nil {
		return err
	}
	folder, ok := named(folders, strings.TrimSuffix(set[folderSetting], "/"), func(f rootFolder) string { return strings.TrimSuffix(f.Path, "/") })
	if !ok {
		return fmt.Errorf("radarr has no root folder at %q", set[folderSetting])
	}
	_, err = api.Bytes(ctx, provider.Request{Method: http.MethodPost, Path: "/api/v3/movie", Header: api.header(), Body: movie{
		Title: w.Title, TMDBID: tmdb, Year: w.Year, QualityProfileID: profile.ID, RootFolderPath: folder.Path,
		Monitored: true, MinimumAvailability: "released", AddOptions: addOptions{SearchForMovie: true},
	}})
	return said(err)
}

// named answers the one of all named name, ignoring case, or the first where name is "".
func named[T any](all []T, name string, of func(T) string) (T, bool) {
	for _, v := range all {
		if name == "" || strings.EqualFold(of(v), name) {
			return v, true
		}
	}
	var none T
	return none, false
}

type radarr struct {
	provider.Client
	key string
}

func (r radarr) header() http.Header { return http.Header{"X-Api-Key": {r.key}} }

func (r radarr) get(ctx context.Context, path string, q url.Values, out any) error {
	return said(r.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: r.header()}, out))
}

// said is a refusal in Radarr's words, as it answers what it will not do: a list of what it found
// wrong with what it was sent.
func said(err error) error {
	refusal, refused := errors.AsType[*provider.Refusal](err)
	if !refused || refusal.Code >= http.StatusInternalServerError {
		return err
	}
	var wrong []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(refusal.Body, &wrong) != nil || len(wrong) == 0 || wrong[0].ErrorMessage == "" {
		return err
	}
	return fmt.Errorf("radarr: %s: %w", wrong[0].ErrorMessage, err)
}
