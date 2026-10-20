// Package arr speaks what Radarr and Sonarr share: an address and API key, the quality profile and
// root folder a title is added with, and how they say what they will not do.
package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	addressSetting = "address"
	keySetting     = "api_key"
	profileSetting = "quality_profile"
	folderSetting  = "root_folder"
)

// Settings are what an admin sets for one: where it is, its key, and the quality profile and root
// folder titles are added with, its first of each where unset. example is its address as an admin
// would type it.
func Settings(name, example string) []provider.Setting {
	return []provider.Setting{
		{Key: addressSetting, Name: "Address, such as " + example, Required: true},
		{Key: keySetting, Name: "API key", Secret: true, Required: true},
		{Key: profileSetting, Name: "Quality profile, by name; " + name + "'s first where unset"},
		{Key: folderSetting, Name: "Root folder, by path; " + name + "'s first where unset"},
	}
}

// Client asks one, as an admin set it.
type Client struct {
	api provider.Client
	key string
	set map[string]string
}

// New is name, as settings say it is: ErrNotConfigured without its address and key.
func New(ctx context.Context, name string, settings provider.Settings) (Client, error) {
	set, err := settings(ctx)
	if err != nil {
		return Client{}, err
	}
	if set[addressSetting] == "" || set[keySetting] == "" {
		return Client{}, provider.ErrNotConfigured
	}
	return Client{api: provider.Client{Name: name, Base: set[addressSetting]}, key: set[keySetting], set: set}, nil
}

func (c Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	return c.said(c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: c.header()}, out))
}

func (c Client) Post(ctx context.Context, path string, body any) error {
	_, err := c.api.Bytes(ctx, provider.Request{Method: http.MethodPost, Path: path, Header: c.header(), Body: body})
	return c.said(err)
}

func (c Client) header() http.Header { return http.Header{"X-Api-Key": {c.key}} }

// Place is where a title is added: the id of the quality profile an admin named, ignoring case,
// and the root folder at the path an admin named; the first of each where none is.
func (c Client) Place(ctx context.Context) (profile int, folder string, err error) {
	type qualityProfile struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	var profiles []qualityProfile
	if err := c.Get(ctx, "/api/v3/qualityprofile", nil, &profiles); err != nil {
		return 0, "", err
	}
	name := c.set[profileSetting]
	i := slices.IndexFunc(profiles, func(p qualityProfile) bool { return name == "" || strings.EqualFold(p.Name, name) })
	if i < 0 {
		return 0, "", fmt.Errorf("%s has no quality profile named %q", c.api.Name, name)
	}
	type rootFolder struct {
		Path string `json:"path"`
	}
	var folders []rootFolder
	if err := c.Get(ctx, "/api/v3/rootfolder", nil, &folders); err != nil {
		return 0, "", err
	}
	path := strings.TrimSuffix(c.set[folderSetting], "/")
	j := slices.IndexFunc(folders, func(f rootFolder) bool {
		return path == "" || strings.EqualFold(strings.TrimSuffix(f.Path, "/"), path)
	})
	if j < 0 {
		return 0, "", fmt.Errorf("%s has no root folder at %q", c.api.Name, c.set[folderSetting])
	}
	return profiles[i].ID, folders[j].Path, nil
}

// said is a refusal in its words, as it answers what it will not do: a list of what it found wrong
// with what it was sent.
func (c Client) said(err error) error {
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
	return fmt.Errorf("%s: %s: %w", c.api.Name, wrong[0].ErrorMessage, err)
}
