package historyimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// jellyfinPage is how many items one request to Jellyfin or Emby lists.
const jellyfinPage = 500

// jellyfin reads what a Jellyfin or Emby user has watched; the two differ in the header a client
// signs in by and where a user's items are listed.
type jellyfin struct {
	kind  domain.ImportSource
	base  string
	login store.ImportLogin
	// header is the one the client and its token are named in.
	header string
	// items is the path a user's items are listed at, {user} standing for the user's id.
	items string
}

type jellyfinItems struct {
	Items            []jellyfinItem `json:"Items"`
	TotalRecordCount int            `json:"TotalRecordCount"`
}

type jellyfinItem struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	SeriesName        string            `json:"SeriesName"`
	SeriesID          string            `json:"SeriesId"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	IndexNumber       int               `json:"IndexNumber"`
	ParentIndexNumber int               `json:"ParentIndexNumber"`
	UserData          struct {
		PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
		PlayCount             int    `json:"PlayCount"`
		LastPlayedDate        string `json:"LastPlayedDate"`
		Played                bool   `json:"Played"`
	} `json:"UserData"`
}

func (j jellyfin) send(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var sent io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		sent = bytes.NewReader(b)
	}
	target := j.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, sent)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	auth := `MediaBrowser Client="photon-server", Device="photon-server", DeviceId="photon-server-history-import", Version="1"`
	if j.login.Token != "" {
		auth += `, Token="` + j.login.Token + `"`
	}
	req.Header.Set(j.header, auth)
	c := provider.Client{Name: string(j.kind), HTTP: client}
	if out == nil {
		_, err = c.Bytes(req)
		return err
	}
	return c.Do(req, out)
}

func (j jellyfin) connect(ctx context.Context, c Credentials) (store.ImportLogin, error) {
	if c.Username == "" {
		return store.ImportLogin{}, fmt.Errorf("%w: a %s server is signed in to by a user's name and password", ErrRefused, j.kind)
	}
	var signed struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			ID string `json:"Id"`
		} `json:"User"`
	}
	err := j.send(ctx, http.MethodPost, "/Users/AuthenticateByName", nil,
		map[string]string{"Username": c.Username, "Pw": c.Password}, &signed)
	return store.ImportLogin{User: signed.User.ID, Token: signed.AccessToken}, err
}

// signOut ends the session signing in began, so it does not linger among the source's devices.
// The server answers 204 No Content.
func (j jellyfin) signOut(ctx context.Context) error {
	err := j.send(ctx, http.MethodPost, "/Sessions/Logout", nil, nil, nil)
	if r, ok := errors.AsType[*provider.Refusal](err); ok && r.Code == http.StatusNoContent {
		return nil
	}
	return err
}

// all lists every item a query finds, a page at a time.
func (j jellyfin) all(ctx context.Context, query url.Values) ([]jellyfinItem, error) {
	query.Set("userId", j.login.User)
	query.Set("Recursive", "true")
	query.Set("Fields", "ProviderIds")
	query.Set("Limit", strconv.Itoa(jellyfinPage))
	path := strings.ReplaceAll(j.items, "{user}", url.PathEscape(j.login.User))
	var out []jellyfinItem
	for {
		query.Set("StartIndex", strconv.Itoa(len(out)))
		var page jellyfinItems
		if err := j.send(ctx, http.MethodGet, path, query, nil, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if len(page.Items) == 0 || len(out) >= page.TotalRecordCount {
			return out, nil
		}
	}
}

func (j jellyfin) entries(ctx context.Context) ([]entry, error) {
	series, err := j.all(ctx, url.Values{"IncludeItemTypes": {"Series"}})
	if err != nil {
		return nil, err
	}
	showIDs := map[string]map[domain.Provider]string{}
	for _, s := range series {
		showIDs[s.ID] = providerIDs(s.ProviderIDs)
	}
	var out []entry
	seen := map[string]bool{}
	// A title played again since and stopped part way is both.
	for _, filter := range []string{"IsPlayed", "IsResumable"} {
		items, err := j.all(ctx, url.Values{
			"IncludeItemTypes": {"Movie,Episode"}, "Filters": {filter}, "EnableUserData": {"true"},
		})
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			e := entry{
				title: it.Name, kind: domain.ItemMovie, ids: providerIDs(it.ProviderIDs),
				position: time.Duration(it.UserData.PlaybackPositionTicks) * 100, at: jellyfinDate(it.UserData.LastPlayedDate),
			}
			if it.Type == "Episode" {
				e.title = fmt.Sprintf("%s S%02dE%02d", it.SeriesName, it.ParentIndexNumber, it.IndexNumber)
				e.kind, e.ids, e.season, e.episode = domain.ItemEpisode, showIDs[it.SeriesID], it.ParentIndexNumber, it.IndexNumber
			}
			// A title marked unplayed keeps its count of plays.
			if it.UserData.Played {
				e.plays = max(it.UserData.PlayCount, 1)
			}
			out = append(out, e)
		}
	}
	return out, nil
}

// jellyfinDate reads a date as Jellyfin and Emby write one, in UTC with or without its zone; zero
// for none.
func jellyfinDate(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	t, err := time.Parse("2006-01-02T15:04:05.9999999", s)
	if err != nil {
		return time.Time{}
	}
	return t
}
