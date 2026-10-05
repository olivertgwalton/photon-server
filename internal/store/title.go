package store

import (
	"cmp"
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"time"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// TitlePage is everything a title's page shows, read in one go and answered to clients as it
// stands: a film's or episode's versions, a show's seasons, a season's episodes, and whatever
// extras and videos the title has.
type TitlePage struct {
	ID            uuid.UUID                  `json:"id"`
	Kind          domain.ItemKind            `json:"kind"`
	Title         string                     `json:"title"`
	OriginalTitle string                     `json:"original_title,omitzero"`
	Overview      string                     `json:"overview,omitzero"`
	Tagline       string                     `json:"tagline,omitzero"`
	Certificate   string                     `json:"certificate,omitzero"`
	Year          int                        `json:"year,omitzero"`
	ReleaseDate   domain.Date                `json:"release_date,omitzero"`
	Genres        []string                   `json:"genres,omitzero"`
	Studios       []string                   `json:"studios,omitzero"`
	IDs           map[domain.Provider]string `json:"ids,omitzero"`
	Ratings       []RatingRef                `json:"ratings,omitzero"`
	// Collections are the box sets it is in.
	Collections   []TitleRef    `json:"collections,omitzero"`
	AddedAt       time.Time     `json:"added_at"`
	SeasonNumber  *int          `json:"season_number,omitzero"`
	EpisodeNumber *int          `json:"episode_number,omitzero"`
	EpisodeEnd    *int          `json:"episode_end,omitzero"`
	Show          *TitleRef     `json:"show,omitzero"`
	Season        *TitleRef     `json:"season,omitzero"`
	Versions      []VersionPage `json:"versions,omitzero"`
	Seasons       []SeasonCard  `json:"seasons,omitzero"`
	Episodes      []EpisodeCard `json:"episodes,omitzero"`
	Extras        []ExtraCard   `json:"extras,omitzero"`
	Videos        []VideoLink   `json:"videos,omitzero"`
	// State is what the profile asking has made of it.
	State TitleState `json:"state,omitzero"`
	// Artwork is the title's pictures by kind, best first, by id: /api/v1/artwork/{id}.
	Artwork map[domain.ArtworkKind][]uuid.UUID `json:"artwork,omitzero"`
}

type TitleRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// VersionPage is one copy: what it is, the tracks in it and the subtitles beside it. A copy whose
// files are gone says since when.
type VersionPage struct {
	ID           uuid.UUID     `json:"id"`
	Edition      string        `json:"edition,omitzero"`
	Label        string        `json:"label,omitzero"`
	Container    string        `json:"container"`
	DurationMS   int64         `json:"duration_ms"`
	SizeBytes    int64         `json:"size_bytes"`
	BitrateKbps  int           `json:"bitrate_kbps,omitzero"`
	Parts        int           `json:"parts"`
	MissingSince *time.Time    `json:"missing_since,omitzero"`
	Streams      []StreamPage  `json:"streams"`
	Subtitles    []SubtitleRef `json:"subtitles,omitzero"`
	Chapters     []ChapterRef  `json:"chapters,omitzero"`
}

// StreamPage is a track of a copy's first part; the parts of one copy are cut from one master.
type StreamPage struct {
	Index           int               `json:"index"`
	Kind            domain.StreamKind `json:"kind"`
	Codec           string            `json:"codec"`
	Profile         string            `json:"profile,omitzero"`
	Language        string            `json:"language,omitzero"`
	Title           string            `json:"title,omitzero"`
	Default         bool              `json:"default,omitzero"`
	Forced          bool              `json:"forced,omitzero"`
	HearingImpaired bool              `json:"hearing_impaired,omitzero"`
	Commentary      bool              `json:"commentary,omitzero"`
	Width           int               `json:"width,omitzero"`
	Height          int               `json:"height,omitzero"`
	FrameRate       float64           `json:"frame_rate,omitzero"`
	BitDepth        int16             `json:"bit_depth,omitzero"`
	Level           int               `json:"level,omitzero"`
	Range           domain.Range      `json:"range,omitzero"`
	DVProfile       int16             `json:"dv_profile,omitzero"`
	Channels        int               `json:"channels,omitzero"`
	ChannelLayout   string            `json:"channel_layout,omitzero"`
	SampleRate      int               `json:"sample_rate,omitzero"`
	BitrateKbps     int               `json:"bitrate_kbps,omitzero"`
}

type SubtitleRef struct {
	Codec           string `json:"codec"`
	Language        string `json:"language,omitzero"`
	Title           string `json:"title,omitzero"`
	Default         bool   `json:"default,omitzero"`
	Forced          bool   `json:"forced,omitzero"`
	HearingImpaired bool   `json:"hearing_impaired,omitzero"`
}

// RatingRef is what a site's readers or critics make of a title, out of 100.
type RatingRef struct {
	Site  domain.RatingSite `json:"site"`
	Score float64           `json:"score"`
	Votes int               `json:"votes,omitzero"`
}

// ChapterRef is a chapter on the copy's whole timeline, across its parts.
type ChapterRef struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Title   string `json:"title,omitzero"`
}

type SeasonCard struct {
	ID       uuid.UUID   `json:"id"`
	Number   int         `json:"number"`
	Title    string      `json:"title"`
	Overview string      `json:"overview,omitzero"`
	Year     int         `json:"year,omitzero"`
	Aired    domain.Date `json:"release_date,omitzero"`
	Episodes int         `json:"episodes"`
	Poster   uuid.UUID   `json:"poster,omitzero"`
	State    TitleState  `json:"state,omitzero"`
}

type EpisodeCard struct {
	ID         uuid.UUID   `json:"id"`
	Number     *int        `json:"episode_number,omitzero"`
	End        *int        `json:"episode_end,omitzero"`
	Title      string      `json:"title"`
	Overview   string      `json:"overview,omitzero"`
	Aired      domain.Date `json:"release_date,omitzero"`
	DurationMS int64       `json:"duration_ms,omitzero"`
	Thumb      uuid.UUID   `json:"thumb,omitzero"`
	State      TitleState  `json:"state,omitzero"`
}

type ExtraCard struct {
	ID         uuid.UUID        `json:"id"`
	Kind       domain.ExtraKind `json:"extra_kind"`
	Title      string           `json:"title"`
	DurationMS int64            `json:"duration_ms,omitzero"`
}

// VideoLink is a provider's link to a video hosted elsewhere.
type VideoLink struct {
	Kind      domain.ExtraKind `json:"extra_kind"`
	Site      string           `json:"site"`
	Key       string           `json:"key"`
	Name      string           `json:"name"`
	Language  string           `json:"language,omitzero"`
	Published *time.Time       `json:"published_at,omitzero"`
}

// Title answers a title's page for a profile, or ErrNotFound.
func (s *Store) Title(ctx context.Context, profile, id uuid.UUID) (TitlePage, error) {
	i := s.q.Item
	item, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TitlePage{}, ErrNotFound
	}
	if err != nil {
		return TitlePage{}, err
	}
	p := TitlePage{
		ID: id, Kind: item.Kind, Title: item.Title, OriginalTitle: deref(item.OriginalTitle),
		Overview: deref(item.Overview), Tagline: deref(item.Tagline), Certificate: deref(item.Certificate),
		Year: deref(item.Year), ReleaseDate: date(item.ReleaseDate), Genres: item.Genres, Studios: item.Studios,
		AddedAt: item.AddedAt, SeasonNumber: item.SeasonNumber, EpisodeNumber: item.EpisodeNumber,
		EpisodeEnd: item.EpisodeEnd,
	}
	if p.IDs, err = s.externalIDs(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if err := s.parents(ctx, item, &p); err != nil {
		return TitlePage{}, err
	}
	ratings, err := s.ratings(ctx, item.ID)
	if err != nil {
		return TitlePage{}, err
	}
	for _, r := range ratings {
		p.Ratings = append(p.Ratings, RatingRef(r))
	}
	if p.Collections, err = s.collectionsOf(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	switch item.Kind {
	case domain.ItemShow:
		p.Seasons, err = s.seasons(ctx, profile, item.ID)
	case domain.ItemSeason:
		p.Episodes, err = s.episodes(ctx, profile, item.ID)
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		p.Versions, err = s.versions(ctx, item.ID)
	case domain.ItemCollection:
	}
	if err != nil {
		return TitlePage{}, err
	}
	if p.Extras, err = s.extras(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Videos, err = s.videos(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	pictures, err := s.pictureOrder(ctx, []*model.Item{item})
	if err != nil {
		return TitlePage{}, err
	}
	p.Artwork = pictures[item.ID]
	states, err := s.states(ctx, profile, []*model.Item{item})
	p.State = states[item.ID]
	return p, err
}

func (s *Store) externalIDs(ctx context.Context, item model.UUID) (map[domain.Provider]string, error) {
	e := s.q.ExternalID
	rows, err := e.WithContext(ctx).Where(e.ItemID.Eq(item)).Find()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make(map[domain.Provider]string, len(rows))
	for _, r := range rows {
		ids[r.Provider] = r.Value
	}
	return ids, nil
}

// parents names the show a season belongs to, and the season and show an episode does.
func (s *Store) parents(ctx context.Context, item *model.Item, p *TitlePage) error {
	i := s.q.Item
	parent := item.ParentID
	for parent != nil && (item.Kind == domain.ItemSeason || item.Kind == domain.ItemEpisode) {
		row, err := i.WithContext(ctx).Where(i.ID.Eq(*parent)).Take()
		if err != nil {
			return err
		}
		ref := &TitleRef{ID: uuid.UUID(row.ID), Title: row.Title}
		switch row.Kind {
		case domain.ItemSeason:
			p.Season = ref
		case domain.ItemShow:
			p.Show = ref
		case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection:
		}
		parent = row.ParentID
	}
	return nil
}

func (s *Store) seasons(ctx context.Context, profile uuid.UUID, show model.UUID) ([]SeasonCard, error) {
	i := s.q.Item
	rows, err := i.WithContext(ctx).Where(i.ParentID.Eq(show), i.Kind.Eq(string(domain.ItemSeason))).
		Order(i.SeasonNumber).Find()
	if err != nil {
		return nil, err
	}
	var counts []struct {
		ParentID model.UUID
		N        int
	}
	err = i.WithContext(ctx).Select(i.ParentID, i.ID.Count().As("n")).
		Where(i.ParentID.In(ids(rows)...), i.Kind.Eq(string(domain.ItemEpisode))).Group(i.ParentID).Scan(&counts)
	if err != nil {
		return nil, err
	}
	episodes := map[model.UUID]int{}
	for _, c := range counts {
		episodes[c.ParentID] = c.N
	}
	pictures, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	states, err := s.states(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	out := make([]SeasonCard, len(rows))
	for n, r := range rows {
		out[n] = SeasonCard{
			ID: uuid.UUID(r.ID), Number: deref(r.SeasonNumber), Title: r.Title, Overview: deref(r.Overview),
			Year: deref(r.Year), Aired: date(r.ReleaseDate), Episodes: episodes[r.ID],
			Poster: first(pictures[r.ID][domain.ArtworkPoster]), State: states[r.ID],
		}
	}
	return out, nil
}

func (s *Store) episodes(ctx context.Context, profile uuid.UUID, season model.UUID) ([]EpisodeCard, error) {
	i := s.q.Item
	rows, err := i.WithContext(ctx).Where(i.ParentID.Eq(season), i.Kind.Eq(string(domain.ItemEpisode))).
		Order(i.EpisodeNumber, i.AirDate, i.SortTitle).Find()
	if err != nil {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	pictures, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	states, err := s.states(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	out := make([]EpisodeCard, len(rows))
	for n, r := range rows {
		aired := r.ReleaseDate
		if aired == nil {
			aired = r.AirDate
		}
		out[n] = EpisodeCard{
			ID: uuid.UUID(r.ID), Number: r.EpisodeNumber, End: r.EpisodeEnd, Title: r.Title,
			Overview: deref(r.Overview), Aired: date(aired), DurationMS: lengths[r.ID],
			Thumb: first(pictures[r.ID][domain.ArtworkThumb]), State: states[r.ID],
		}
	}
	return out, nil
}

func (s *Store) extras(ctx context.Context, owner model.UUID) ([]ExtraCard, error) {
	i := s.q.Item
	rows, err := i.WithContext(ctx).Where(i.ParentID.Eq(owner), i.Kind.Eq(string(domain.ItemExtra))).
		Order(i.ExtraKind, i.SortTitle).Find()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	out := make([]ExtraCard, len(rows))
	for n, r := range rows {
		out[n] = ExtraCard{ID: uuid.UUID(r.ID), Kind: deref(r.ExtraKind), Title: r.Title, DurationMS: lengths[r.ID]}
	}
	return out, nil
}

// durations answers how long each title runs: its longest copy still on disk.
func (s *Store) durations(ctx context.Context, items []driver.Valuer) (map[model.UUID]int64, error) {
	v := s.q.Version
	rows, err := v.WithContext(ctx).Where(v.ItemID.In(items...), v.MissingSince.IsNull()).Find()
	if err != nil {
		return nil, err
	}
	out := map[model.UUID]int64{}
	for _, r := range rows {
		out[r.ItemID] = max(out[r.ItemID], r.DurationMS)
	}
	return out, nil
}

func (s *Store) videos(ctx context.Context, item model.UUID) ([]VideoLink, error) {
	rv := s.q.RemoteVideo
	rows, err := rv.WithContext(ctx).Where(rv.ItemID.Eq(item)).Order(rv.Source, rv.Position).Find()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	out := make([]VideoLink, len(rows))
	for n, r := range rows {
		out[n] = VideoLink{
			Kind: r.Kind, Site: r.Site, Key: r.Key, Name: r.Name, Language: deref(r.Language), Published: r.PublishedAt,
		}
	}
	return out, nil
}

// versions answers a film's or episode's copies, those on disk first, the longest first.
func (s *Store) versions(ctx context.Context, item model.UUID) ([]VersionPage, error) {
	v, pt, st, ch, sf := s.q.Version, s.q.Part, s.q.Stream, s.q.Chapter, s.q.SubtitleFile
	rows, err := v.WithContext(ctx).Where(v.ItemID.Eq(item)).Find()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	slices.SortStableFunc(rows, func(a, b *model.Version) int {
		if (a.MissingSince == nil) != (b.MissingSince == nil) {
			return map[bool]int{true: -1, false: 1}[a.MissingSince == nil]
		}
		return cmp.Compare(b.DurationMS, a.DurationMS)
	})
	vids := make([]driver.Valuer, len(rows))
	for n, r := range rows {
		vids[n] = r.ID
	}
	parts, err := pt.WithContext(ctx).Where(pt.VersionID.In(vids...)).Order(pt.VersionID, pt.Idx).Find()
	if err != nil {
		return nil, err
	}
	byVersion := map[model.UUID][]*model.Part{}
	var pids []driver.Valuer
	for _, p := range parts {
		byVersion[p.VersionID] = append(byVersion[p.VersionID], p)
		pids = append(pids, p.ID)
	}
	streams, err := st.WithContext(ctx).Where(st.PartID.In(pids...)).Order(st.PartID, st.Idx).Find()
	if err != nil {
		return nil, err
	}
	chapters, err := ch.WithContext(ctx).Where(ch.PartID.In(pids...)).Order(ch.PartID, ch.Idx).Find()
	if err != nil {
		return nil, err
	}
	subs, err := sf.WithContext(ctx).Where(sf.VersionID.In(vids...)).Order(sf.RelPath).Find()
	if err != nil {
		return nil, err
	}
	out := make([]VersionPage, len(rows))
	for n, r := range rows {
		vp := VersionPage{
			ID: uuid.UUID(r.ID), Edition: deref(r.Edition), Label: deref(r.Label), Container: r.Container,
			DurationMS: r.DurationMS, SizeBytes: r.SizeBytes, BitrateKbps: r.BitrateKbps,
			Parts: len(byVersion[r.ID]), MissingSince: r.MissingSince, Streams: []StreamPage{},
		}
		for k, p := range byVersion[r.ID] {
			for _, c := range chapters {
				if c.PartID == p.ID {
					vp.Chapters = append(vp.Chapters, ChapterRef{
						StartMS: p.OffsetMS + c.StartMS, EndMS: p.OffsetMS + c.EndMS, Title: deref(c.Title),
					})
				}
			}
			if k > 0 {
				continue
			}
			for _, t := range streams {
				if t.PartID == p.ID {
					vp.Streams = append(vp.Streams, streamPage(t))
				}
			}
		}
		for _, f := range subs {
			if f.VersionID == r.ID {
				vp.Subtitles = append(vp.Subtitles, SubtitleRef{
					Codec: f.Codec, Language: deref(f.Language), Title: deref(f.Title), Default: f.IsDefault,
					Forced: f.Forced, HearingImpaired: f.HearingImpaired,
				})
			}
		}
		out[n] = vp
	}
	return out, nil
}

func streamPage(t *model.Stream) StreamPage {
	return StreamPage{
		Index: t.Idx, Kind: t.Kind, Codec: t.Codec, Profile: deref(t.Profile), Language: deref(t.Language),
		Title: deref(t.Title), Default: t.IsDefault, Forced: t.Forced, HearingImpaired: t.HearingImpaired,
		Commentary: t.Commentary, Width: deref(t.Width), Height: deref(t.Height), FrameRate: deref(t.FrameRate),
		BitDepth: deref(t.BitDepth), Level: deref(t.Level),
		Range: deref(t.VideoRange), DVProfile: deref(t.DVProfile), Channels: deref(t.Channels),
		ChannelLayout: deref(t.ChannelLayout), SampleRate: deref(t.SampleRate), BitrateKbps: deref(t.BitrateKbps),
	}
}

// ids is the items' ids as gen's In takes them.
func ids(rows []*model.Item) []driver.Valuer {
	out := make([]driver.Valuer, len(rows))
	for n, r := range rows {
		out[n] = r.ID
	}
	return out
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func date(t *time.Time) domain.Date {
	if t == nil {
		return domain.Date{}
	}
	return domain.Date(*t)
}

func first(ids []uuid.UUID) uuid.UUID {
	if len(ids) == 0 {
		return uuid.UUID{}
	}
	return ids[0]
}
