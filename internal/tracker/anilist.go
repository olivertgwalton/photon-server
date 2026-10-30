package tracker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// aniListPaste is how long a profile has to bring back the token AniList shows it.
	aniListPaste = 15 * time.Minute
	// aniListGrant is how long AniList's tokens last; none is refreshed.
	aniListGrant = 365 * 24 * time.Hour
	// whole is an entry watched to its end, as a film is.
	whole = math.MaxInt
)

// aniList is AniList's API. It has no device flow: a profile signs in on AniList's page for an app
// registered with AniList's PIN address as its redirect, and AniList shows it a token to bring
// back. AniList keeps no plays, only how far into each entry an account is, which the history push
// moves on, through the anime lists, as episodes are watched.
type aniList struct {
	api   provider.Client
	lists animelist.Lists
}

func newAniList(l animelist.Lists, pauses provider.Pauses) aniList {
	return aniList{api: provider.Client{Name: "anilist", Base: "https://graphql.anilist.co", Pauses: pauses}, lists: l}
}

func (aniList) flow() LinkFlow { return LinkToken }

// code answers the page a profile signs in on, which AniList shows its token on. It is never asked
// after: the profile brings the token.
func (aniList) code(_ context.Context, clientID string) (kv.TrackerLink, error) {
	page := "https://anilist.co/api/v2/oauth/authorize?" + url.Values{"client_id": {clientID}, "response_type": {"token"}}.Encode()
	return kv.TrackerLink{VerificationURI: page, VerificationURIComplete: page, Interval: aniListPaste, Expires: time.Now().Add(aniListPaste)}, nil
}

func (aniList) token(context.Context, string, string) (store.TrackerTokens, error) {
	return store.TrackerTokens{}, errPending
}

func (aniList) pasted(token string) store.TrackerTokens {
	return store.TrackerTokens{Access: token, Expires: time.Now().Add(aniListGrant)}
}

func (c aniList) username(ctx context.Context, _, access string) (string, error) {
	var body struct {
		Data struct {
			Viewer struct {
				Name string `json:"name"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	err := c.ask(ctx, access, `{ Viewer { name } }`, nil, &body)
	return body.Data.Viewer.Name, err
}

// revoke does nothing: AniList's grants are revoked on its site alone.
func (aniList) revoke(context.Context, string, store.TrackerTokens) error { return nil }

// refresh is not AniList's: its year-long grant ends, and the profile links it again.
func (aniList) refresh(context.Context, string, store.TrackerTokens) (store.TrackerTokens, error) {
	return store.TrackerTokens{}, errGrantGone
}

func (aniList) scrobble(context.Context, string, string, action, play) error { return errNoPlays }

// history moves each entry an account watched on to the furthest of its episodes watched, never
// back, and completes it at its last; a film watched completes its entry. Episodes unwatched leave
// AniList as it is, which keeps how far an account got rather than which episodes it watched.
func (c aniList) history(ctx context.Context, _, access string, w historyWrite, h history) error {
	if w == historyRemove {
		return nil
	}
	reached, err := c.reached(ctx, h)
	if err != nil || len(reached) == 0 {
		return err
	}
	media := make([]int, 0, len(reached))
	for id := range reached {
		media = append(media, id)
	}
	var viewer struct {
		Data struct {
			Viewer struct {
				ID int `json:"id"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	if err := c.ask(ctx, access, `{ Viewer { id } }`, nil, &viewer); err != nil {
		return err
	}
	var listed struct {
		Data struct {
			Page struct {
				MediaList []struct {
					MediaID  int    `json:"mediaId"`
					Progress int    `json:"progress"`
					Status   string `json:"status"`
				} `json:"mediaList"`
				Media []struct {
					ID       int `json:"id"`
					Episodes int `json:"episodes"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	err = c.ask(ctx, access, `query($user: Int, $ids: [Int]) { Page(perPage: 50) {
		mediaList(userId: $user, mediaId_in: $ids, type: ANIME) { mediaId progress status }
		media(id_in: $ids, type: ANIME) { id episodes } } }`,
		map[string]any{"user": viewer.Data.Viewer.ID, "ids": media}, &listed)
	if err != nil {
		return err
	}
	type entry struct {
		progress int
		status   string
	}
	kept := map[int]entry{}
	for _, l := range listed.Data.Page.MediaList {
		kept[l.MediaID] = entry{l.Progress, l.Status}
	}
	episodes := map[int]int{}
	for _, m := range listed.Data.Page.Media {
		episodes[m.ID] = m.Episodes
	}
	var errs []error
	for id, progress := range reached {
		was := kept[id]
		if was.status == "COMPLETED" || was.status == "REPEATING" || progress <= was.progress {
			continue
		}
		status := "CURRENT"
		if progress == whole || episodes[id] > 0 && progress >= episodes[id] {
			progress, status = cmp.Or(episodes[id], 1), "COMPLETED"
		}
		errs = append(errs, c.ask(ctx, access, `mutation($id: Int, $progress: Int, $status: MediaListStatus) {
			SaveMediaListEntry(mediaId: $id, progress: $progress, status: $status) { id } }`,
			map[string]any{"id": id, "progress": progress, "status": status}, nil))
	}
	return errors.Join(errs...)
}

// reached answers how far into each AniList entry the films and episodes watched reach: an
// episode's number in the entry the anime lists place it in, and a film's entry whole. A
// special, the anime lists' or a show's, moves no entry on, as AniList counts only the regular.
func (c aniList) reached(ctx context.Context, h history) (map[int]int, error) {
	out := map[int]int{}
	for _, m := range h.Movies {
		e, ok, err := animelist.Find(ctx, c.lists, domain.ItemMovie, providerIDs(m.IDs))
		if err != nil {
			return nil, err
		}
		if ok && e.AniList != 0 {
			out[e.AniList] = whole
		}
	}
	for _, s := range h.Shows {
		show, err := strconv.Atoi(providerIDs(s.IDs)[s.numbering])
		if err != nil {
			continue
		}
		entries, err := c.lists.AnimeShow(ctx, s.numbering, show)
		if err != nil {
			return nil, err
		}
		for _, season := range s.Seasons {
			in := season.Number
			switch {
			case in == 0:
				continue
			case s.order == domain.OrderAbsolute:
				in = animelist.Absolute
			case s.order != domain.OrderAired:
				continue
			}
			for _, ep := range season.Episodes {
				if place, ok := animelist.Episode(entries, s.numbering, in, ep.Number); ok && !place.Special && place.Entry.AniList != 0 {
					out[place.Entry.AniList] = max(out[place.Entry.AniList], place.Episode)
				}
			}
		}
	}
	return out, nil
}

// providerIDs are a title's ids as trackers are told them, as the server keeps them.
func providerIDs(ids map[string]any) map[domain.Provider]string {
	out := make(map[domain.Provider]string, len(ids))
	for p, v := range ids {
		out[domain.Provider(p)] = fmt.Sprint(v)
	}
	return out
}

// ask sends AniList a query or mutation for the account access is, decoding its data into out.
func (c aniList) ask(ctx context.Context, access, query string, vars map[string]any, out any) error {
	r := provider.Request{
		Method: http.MethodPost, Path: "/", Header: http.Header{"Accept": {"application/json"}, "Authorization": {"Bearer " + access}},
		Body: map[string]any{"query": query, "variables": vars},
	}
	if out == nil {
		_, err := c.api.Bytes(ctx, r)
		return err
	}
	return c.api.Do(ctx, r, out)
}
