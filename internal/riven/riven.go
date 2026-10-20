// Package riven asks riven to fetch the titles a library lacks that its profiles ask for, through
// riven's GraphQL API, as Seerr hands it requests. riven finds a release and writes it into the
// library's folder, where a scan finds it.
package riven

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	addressSetting = "address"
	keySetting     = "api_key"
)

// requestedBy is how riven names where its requests came from.
const requestedBy = "photon"

// Client asks the riven at the address an admin sets.
type Client struct {
	settings provider.Settings
}

func New(settings provider.Settings) *Client {
	return &Client{settings: settings}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceRiven, Name: "riven", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{
			{Key: addressSetting, Name: "Address, such as http://riven:8080", Required: true},
			{Key: keySetting, Name: "API key", Secret: true, Required: true},
		},
	}
}

// input is riven's MovieRequestInput or ShowRequestInput: riven knows a film by its TMDB or IMDb
// id and a show by its TheTVDB or IMDb id.
type input struct {
	Title             string `json:"title"`
	TMDBID            string `json:"tmdbId,omitempty"`
	TVDBID            string `json:"tvdbId,omitempty"`
	IMDbID            string `json:"imdbId,omitempty"`
	Seasons           []int  `json:"seasons,omitempty"`
	RequestedBy       string `json:"requestedBy"`
	ExternalRequestID string `json:"externalRequestId"`
}

type query struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// answer is riven's RequestItemMutationResponse, under the mutation's name.
type answer struct {
	Data map[string]struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		StatusText string `json:"statusText"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Request asks riven to fetch a film, or a show's seasons, every one where none are named. A title
// riven has been asked for already is taken on as it was.
func (c *Client) Request(ctx context.Context, w domain.Wanted) error {
	set, err := c.settings(ctx)
	if err != nil {
		return err
	}
	if set[addressSetting] == "" || set[keySetting] == "" {
		return provider.ErrNotConfigured
	}
	in := input{Title: w.Title, IMDbID: w.IDs[domain.ProviderIMDb], RequestedBy: requestedBy, ExternalRequestID: w.ID.String()}
	mutation, inputType := "requestMovie", "MovieRequestInput"
	switch w.Kind {
	case domain.ItemMovie:
		in.TMDBID = w.IDs[domain.ProviderTMDB]
		if in.TMDBID == "" && in.IMDbID == "" {
			return errors.New("riven knows a film by its TMDB or IMDb id, and this one has neither")
		}
	case domain.ItemShow:
		mutation, inputType = "requestShow", "ShowRequestInput"
		in.TVDBID, in.Seasons = w.IDs[domain.ProviderTVDB], w.Seasons
		if in.TVDBID == "" && in.IMDbID == "" {
			return errors.New("riven knows a show by its TheTVDB or IMDb id, and this one has neither")
		}
	case domain.ItemSeason, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection:
		return fmt.Errorf("riven fetches films and shows, not a %s", w.Kind)
	}
	q := query{
		Query:     fmt.Sprintf("mutation($input: %s!) { %s(input: $input) { success message statusText } }", inputType, mutation),
		Variables: map[string]any{"input": in},
	}
	api := provider.Client{Name: "riven", Base: set[addressSetting]}
	var out answer
	err = api.Do(ctx, provider.Request{
		Method: http.MethodPost, Path: "/graphql", Header: http.Header{"X-Api-Key": {set[keySetting]}}, Body: q,
	}, &out)
	if err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("riven: %s", out.Errors[0].Message)
	}
	r := out.Data[mutation]
	if !r.Success && r.StatusText != "CONFLICT" {
		return fmt.Errorf("riven: %s", r.Message)
	}
	return nil
}
