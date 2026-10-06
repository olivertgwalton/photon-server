package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

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
	Collections []CollectionCard `json:"collections,omitzero"`
	// Credits are its cast and crew, as the highest-ranked source gives them.
	Credits []CreditRef `json:"credits,omitzero"`
	// Origin is who made a collection: an admin's is changed by hand, a provider's only by it.
	Origin    domain.CollectionOrigin    `json:"origin,omitzero"`
	Placement domain.CollectionPlacement `json:"placement,omitzero"`
	// EpisodeOrder is the order a show's episode files are numbered in.
	EpisodeOrder  domain.EpisodeOrder `json:"episode_order,omitzero"`
	AddedAt       time.Time           `json:"added_at"`
	SeasonNumber  *int                `json:"season_number,omitzero"`
	EpisodeNumber *int                `json:"episode_number,omitzero"`
	EpisodeEnd    *int                `json:"episode_end,omitzero"`
	Show          *TitleRef           `json:"show,omitzero"`
	Season        *TitleRef           `json:"season,omitzero"`
	Versions      []VersionPage       `json:"versions,omitzero"`
	Seasons       []SeasonCard        `json:"seasons,omitzero"`
	Episodes      []EpisodeCard       `json:"episodes,omitzero"`
	Extras        []ExtraCard         `json:"extras,omitzero"`
	Videos        []VideoLink         `json:"videos,omitzero"`
	// State is what the profile asking has made of it.
	State TitleState `json:"state,omitzero"`
	// Artwork is the title's pictures by kind, best first, by id: /api/v1/artwork/{id}.
	Artwork map[domain.ArtworkKind][]uuid.UUID `json:"artwork,omitzero"`
	// Blurhashes are those of its pictures that have one, by id.
	Blurhashes Blurhashes `json:"blurhashes,omitzero"`
	// Themes are the tunes to play under its page, in order, by id: /api/v1/themes/{id}. A season's
	// and an episode's are its show's.
	Themes []uuid.UUID `json:"themes,omitzero"`
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
	Markers      []MarkerRef   `json:"markers,omitzero"`
	// Files are its parts in order, each where it starts on the copy's timeline, by the id the
	// /api/v1/parts/{id} routes take.
	Files []PartRef `json:"files"`
	// Trickplay is the thumbnail sheets of each part that has them; a part's sheets are at
	// /api/v1/parts/{part_id}/trickplay/{n}.
	Trickplay []PartTrickplay `json:"trickplay,omitzero"`
	// DefaultAudioStream and DefaultSubtitleStream or DefaultSubtitleFile are the tracks it plays
	// with unasked, for the profile asking: none where no subtitle comes on.
	DefaultAudioStream    *int       `json:"default_audio_stream,omitzero"`
	DefaultSubtitleStream *int       `json:"default_subtitle_stream,omitzero"`
	DefaultSubtitleFile   *uuid.UUID `json:"default_subtitle_file,omitzero"`
}

// PartRef is one file of a copy.
type PartRef struct {
	ID         uuid.UUID `json:"id"`
	Index      int       `json:"index"`
	SizeBytes  int64     `json:"size_bytes"`
	DurationMS int64     `json:"duration_ms"`
	OffsetMS   int64     `json:"offset_ms"`
}

// PartTrickplay is a part's thumbnail sheets, its thumbnails timed from OffsetMS on the copy's
// timeline.
type PartTrickplay struct {
	PartID   uuid.UUID `json:"part_id"`
	OffsetMS int64     `json:"offset_ms"`
	Trickplay
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
	ID              uuid.UUID `json:"id"`
	Codec           string    `json:"codec"`
	Language        string    `json:"language,omitzero"`
	Title           string    `json:"title,omitzero"`
	Default         bool      `json:"default,omitzero"`
	Forced          bool      `json:"forced,omitzero"`
	HearingImpaired bool      `json:"hearing_impaired,omitzero"`
}

// RatingRef is what a site's readers or critics make of a title, out of 100.
type RatingRef struct {
	Site  domain.RatingSite `json:"site"`
	Score float64           `json:"score"`
	Votes int               `json:"votes,omitzero"`
}

// ChapterRef is a chapter on the copy's whole timeline, across its parts. Image is the address of
// its picture, for those that have one.
type ChapterRef struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Title   string `json:"title,omitzero"`
	Image   string `json:"image,omitzero"`
}

// SignChapterImages signs the address of each chapter's picture, and of each extra's still, as
// sign signs a path.
func (p *TitlePage) SignChapterImages(sign func(path string) string) {
	for _, v := range p.Versions {
		for c := range v.Chapters {
			if ref := &v.Chapters[c]; ref.Image != "" {
				ref.Image = sign(ref.Image)
			}
		}
	}
	for e := range p.Extras {
		if ref := &p.Extras[e]; ref.Image != "" {
			ref.Image = sign(ref.Image)
		}
	}
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
	// Blurhashes are those of its pictures that have one, by id, as on every card.
	Blurhashes Blurhashes `json:"blurhashes,omitzero"`
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
	Blurhashes Blurhashes  `json:"blurhashes,omitzero"`
}

// ExtraCard is a trailer or other extra, pictured by a still of its video where its previews are
// made.
type ExtraCard struct {
	ID         uuid.UUID        `json:"id"`
	Kind       domain.ExtraKind `json:"extra_kind"`
	Title      string           `json:"title"`
	DurationMS int64            `json:"duration_ms,omitzero"`
	Image      string           `json:"image,omitzero"`
}

type CollectionCard struct {
	ID         uuid.UUID  `json:"id"`
	Title      string     `json:"title"`
	Poster     uuid.UUID  `json:"poster,omitzero"`
	Blurhashes Blurhashes `json:"blurhashes,omitzero"`
}

// VideoLink is a provider's link to a video hosted elsewhere, with its site's still of it where
// the site publishes one, served at /api/v1/artwork/{thumb}.
type VideoLink struct {
	Kind      domain.ExtraKind `json:"extra_kind"`
	Site      string           `json:"site"`
	Key       string           `json:"key"`
	Name      string           `json:"name"`
	Language  string           `json:"language,omitzero"`
	Published *time.Time       `json:"published_at,omitzero"`
	Thumb     uuid.UUID        `json:"thumb,omitzero"`
}

// Title answers a title's page for a profile, or ErrNotFound.
func (s *Store) Title(ctx context.Context, profile, id uuid.UUID) (TitlePage, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return TitlePage{}, err
	}
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return TitlePage{}, cmp.Or(err, ErrNotFound)
	}
	p := TitlePage{
		ID: id, Kind: item.Kind, Title: item.Title, OriginalTitle: deref(item.OriginalTitle),
		Overview: deref(item.Overview), Tagline: deref(item.Tagline), Certificate: deref(item.Certificate),
		Year: deref(item.Year), ReleaseDate: domain.Date(deref(item.ReleaseDate)), Genres: item.Genres, Studios: item.Studios,
		AddedAt: item.AddedAt, SeasonNumber: item.SeasonNumber, EpisodeNumber: item.EpisodeNumber,
		EpisodeEnd: item.EpisodeEnd,
	}
	if p.IDs, err = s.externalIDs(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if err := s.parents(ctx, item, &p); err != nil {
		return TitlePage{}, err
	}
	ratings, err := s.ratings(ctx, []*model.Item{item})
	if err != nil {
		return TitlePage{}, err
	}
	for _, r := range ratings[item.ID] {
		p.Ratings = append(p.Ratings, RatingRef(r))
	}
	if p.Collections, err = s.collectionsOf(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Credits, err = s.credits(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	switch item.Kind {
	case domain.ItemShow:
		p.EpisodeOrder = item.EpisodeOrder
		p.Seasons, err = s.seasons(ctx, profile, item.ID)
	case domain.ItemSeason:
		p.Episodes, err = s.episodes(ctx, profile, item.ID)
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		p.Versions, err = s.versions(ctx, item.ID)
	case domain.ItemCollection:
		err = s.pool.QueryRow(ctx, `SELECT origin, placement FROM collections WHERE item_id = $1`, item.ID).Scan(&p.Origin, &p.Placement)
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
	shows, err := s.showsOf(ctx, []*model.Item{item})
	if err != nil {
		return TitlePage{}, err
	}
	pictures, hashes, err := s.picturesWorn(ctx, []*model.Item{item}, shows)
	if err != nil {
		return TitlePage{}, err
	}
	p.Artwork = pictures[item.ID]
	var shown []uuid.UUID
	for _, of := range p.Artwork {
		shown = append(shown, of...)
	}
	p.Blurhashes = blurhashesOf(hashes, shown...)
	owner := id
	if p.Show != nil {
		owner = p.Show.ID
	}
	themes, err := s.themes(ctx, owner)
	if err != nil {
		return TitlePage{}, err
	}
	if len(themes) > 0 {
		p.Themes = themes
	}
	states, err := s.states(ctx, profile, []*model.Item{item})
	p.State = states[item.ID]
	return p, err
}

// readItem answers a title's row, or ErrNotFound.
func readItem(ctx context.Context, q db, id uuid.UUID) (*model.Item, error) {
	row, err := readRow[model.Item](ctx, q, `SELECT `+itemColumns+` FROM items WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// visible reports whether a profile may see a title: a library it has, and a certificate within its
// age; a title it may not is not there to it.
func (s *Store) visible(ctx context.Context, profile, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = $1 AND sees(v, i))`,
		id, profile).Scan(&ok)
	return ok, err
}

// Visible answers those of titles a profile may see, in their order.
func (s *Store) Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	return queryIDs(ctx, s.pool, `
		SELECT t.id FROM unnest($1::uuid[]) WITH ORDINALITY AS t(id, n)
		JOIN items i ON i.id = t.id, viewer($2) v
		WHERE sees(v, i) ORDER BY t.n`, titles, profile)
}

// SameTitles answers a title and those the same as it in other libraries that a profile may see.
func (s *Store) SameTitles(ctx context.Context, profile, title uuid.UUID) ([]uuid.UUID, error) {
	return queryIDs(ctx, s.pool, `
		SELECT t FROM same_title($1) t JOIN items i ON i.id = t, viewer($2) v
		WHERE sees(v, i) ORDER BY t`, title, profile)
}

// HasLibrary reports whether a library is there and a profile may see its titles at all: as
// sees() asks, it has every library where none are listed for it.
func (s *Store) HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $2)
			AND (NOT EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1)
				OR EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1 AND library_id = $2))`,
		profile, lib).Scan(&ok)
	return ok, err
}

func (s *Store) externalIDs(ctx context.Context, item uuid.UUID) (map[domain.Provider]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider, value FROM external_ids WHERE item_id = $1`, item)
	if err != nil {
		return nil, err
	}
	var ids map[domain.Provider]string
	var provider domain.Provider
	var value string
	_, err = pgx.ForEachRow(rows, []any{&provider, &value}, func() error {
		if ids == nil {
			ids = map[domain.Provider]string{}
		}
		ids[provider] = value
		return nil
	})
	return ids, err
}

// parents names the show a season belongs to, and the season and show an episode does.
func (s *Store) parents(ctx context.Context, item *model.Item, p *TitlePage) error {
	parent := item.ParentID
	for parent != nil && (item.Kind == domain.ItemSeason || item.Kind == domain.ItemEpisode) {
		row, err := readItem(ctx, s.pool, *parent)
		if err != nil {
			return err
		}
		ref := &TitleRef{ID: row.ID, Title: row.Title}
		// A season or episode with no certificate of its own wears the nearest it is under.
		p.Certificate = cmp.Or(p.Certificate, deref(row.Certificate))
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

func (s *Store) seasons(ctx context.Context, profile, show uuid.UUID) ([]SeasonCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'season' ORDER BY season_number`, show)
	if err != nil {
		return nil, err
	}
	counted, err := s.pool.Query(ctx, `
		SELECT parent_id, count(*) FROM items WHERE parent_id = ANY($1) AND kind = 'episode' GROUP BY parent_id`,
		ids(rows))
	if err != nil {
		return nil, err
	}
	episodes := map[uuid.UUID]int{}
	var season uuid.UUID
	var n int
	if _, err := pgx.ForEachRow(counted, []any{&season, &n}, func() error {
		episodes[season] = n
		return nil
	}); err != nil {
		return nil, err
	}
	pictures, hashes, err := s.pictureOrder(ctx, rows)
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
			ID: r.ID, Number: deref(r.SeasonNumber), Title: r.Title, Overview: deref(r.Overview),
			Year: deref(r.Year), Aired: domain.Date(deref(r.ReleaseDate)), Episodes: episodes[r.ID],
			Poster: first(pictures[r.ID][domain.ArtworkPoster]), State: states[r.ID],
		}
		out[n].Blurhashes = blurhashesOf(hashes, out[n].Poster)
	}
	return out, nil
}

func (s *Store) episodes(ctx context.Context, profile, season uuid.UUID) ([]EpisodeCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'episode'
		ORDER BY episode_number, air_date, sort_title`, season)
	if err != nil {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	pictures, hashes, err := s.pictureOrder(ctx, rows)
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
			ID: r.ID, Number: r.EpisodeNumber, End: r.EpisodeEnd, Title: r.Title,
			Overview: deref(r.Overview), Aired: domain.Date(deref(aired)), DurationMS: lengths[r.ID],
			Thumb: first(pictures[r.ID][domain.ArtworkThumb]), State: states[r.ID],
		}
		out[n].Blurhashes = blurhashesOf(hashes, out[n].Thumb)
	}
	return out, nil
}

func (s *Store) extras(ctx context.Context, owner uuid.UUID) ([]ExtraCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'extra' ORDER BY extra_kind, sort_title`, owner)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	stills, err := s.stills(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]ExtraCard, len(rows))
	for n, r := range rows {
		out[n] = ExtraCard{ID: r.ID, Kind: deref(r.ExtraKind), Title: r.Title, DurationMS: lengths[r.ID]}
		if at, ok := stills[r.ID]; ok {
			out[n].Image = fmt.Sprintf("/api/v1/parts/%s/chapters/%d/image", at.part, at.idx)
		}
	}
	return out, nil
}

// still is a chapter's picture: its part, and the chapter's idx in it.
type still struct {
	part uuid.UUID
	idx  int
}

// stills answers each title's first chapter picture: of its first copy's first part pictured.
func (s *Store) stills(ctx context.Context, items []*model.Item) (map[uuid.UUID]still, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (v.item_id) v.item_id, p.id, (SELECT min(x) FROM unnest(pv.chapter_images) x)
		FROM versions v JOIN parts p ON p.version_id = v.id JOIN previews pv ON pv.part_id = p.id
		WHERE v.item_id = ANY($1) AND v.missing_since IS NULL AND cardinality(pv.chapter_images) > 0
		ORDER BY v.item_id, v.id, p.idx`, ids(items))
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]still{}
	var item uuid.UUID
	var at still
	_, err = pgx.ForEachRow(rows, []any{&item, &at.part, &at.idx}, func() error {
		out[item] = at
		return nil
	})
	return out, err
}

// durations answers how long each title runs: its longest copy still on disk.
func (s *Store) durations(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, max(duration_ms) FROM versions WHERE item_id = ANY($1) AND missing_since IS NULL
		GROUP BY item_id`, items)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]int64{}
	var item uuid.UUID
	var ms int64
	_, err = pgx.ForEachRow(rows, []any{&item, &ms}, func() error {
		out[item] = ms
		return nil
	})
	return out, err
}

func (s *Store) videos(ctx context.Context, item uuid.UUID) ([]VideoLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, site, key, name, coalesce(language, ''), published_at, thumb_id
		FROM remote_videos WHERE item_id = $1 ORDER BY source, position`, item)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (VideoLink, error) {
		var v VideoLink
		var thumb *uuid.UUID
		err := r.Scan(&v.Kind, &v.Site, &v.Key, &v.Name, &v.Language, &v.Published, &thumb)
		v.Thumb = deref(thumb)
		return v, err
	})
	if len(out) == 0 {
		out = nil
	}
	return out, err
}

// versions answers a film's or episode's copies, those on disk first, the longest first.
func (s *Store) versions(ctx context.Context, item uuid.UUID) ([]VersionPage, error) {
	rows, err := queryRows[model.Version](ctx, s.pool, `SELECT `+versionColumns+` FROM versions WHERE item_id = $1`, item)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	slices.SortStableFunc(rows, func(a, b *model.Version) int {
		if (a.MissingSince == nil) != (b.MissingSince == nil) {
			return map[bool]int{true: -1, false: 1}[a.MissingSince == nil]
		}
		return cmp.Compare(b.DurationMS, a.DurationMS)
	})
	vids := make([]uuid.UUID, len(rows))
	for n, r := range rows {
		vids[n] = r.ID
	}
	parts, err := queryRows[model.Part](ctx, s.pool, `
		SELECT `+partColumns+` FROM parts WHERE version_id = ANY($1) ORDER BY version_id, idx`, vids)
	if err != nil {
		return nil, err
	}
	byVersion := map[uuid.UUID][]*model.Part{}
	var pids []uuid.UUID
	for _, p := range parts {
		byVersion[p.VersionID] = append(byVersion[p.VersionID], p)
		pids = append(pids, p.ID)
	}
	streams, err := queryRows[model.Stream](ctx, s.pool, `
		SELECT `+streamColumns+` FROM streams WHERE part_id = ANY($1) ORDER BY part_id, idx`, pids)
	if err != nil {
		return nil, err
	}
	chapters, err := queryRows[model.Chapter](ctx, s.pool, `
		SELECT `+chapterColumns+` FROM chapters WHERE part_id = ANY($1) ORDER BY part_id, idx`, pids)
	if err != nil {
		return nil, err
	}
	markers, err := queryRows[model.Marker](ctx, s.pool, `SELECT `+markerColumns+` FROM markers WHERE part_id = ANY($1)`, pids)
	if err != nil {
		return nil, err
	}
	subs, err := queryRows[model.SubtitleFile](ctx, s.pool, `
		SELECT `+subtitleFileColumns+` FROM subtitle_files WHERE version_id = ANY($1) ORDER BY rel_path`, vids)
	if err != nil {
		return nil, err
	}
	pictured, sheets, err := s.partPreviews(ctx, parts)
	if err != nil {
		return nil, err
	}
	detection, err := s.markerDetection(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]VersionPage, len(rows))
	for n, r := range rows {
		vp := VersionPage{
			ID: r.ID, Edition: deref(r.Edition), Label: deref(r.Label), Container: r.Container,
			DurationMS: r.DurationMS, SizeBytes: r.SizeBytes, BitrateKbps: r.BitrateKbps,
			Parts: len(byVersion[r.ID]), MissingSince: r.MissingSince, Streams: []StreamPage{},
		}
		for k, p := range byVersion[r.ID] {
			vp.Files = append(vp.Files, PartRef{
				ID: p.ID, Index: int(p.Idx), SizeBytes: p.SizeBytes, DurationMS: p.DurationMS, OffsetMS: p.OffsetMS,
			})
			var own []*model.Chapter
			for _, c := range chapters {
				if c.PartID == p.ID {
					own = append(own, c)
					ref := ChapterRef{StartMS: p.OffsetMS + c.StartMS, EndMS: p.OffsetMS + c.EndMS, Title: deref(c.Title)}
					if slices.Contains(pictured[p.ID], c.Idx) {
						ref.Image = fmt.Sprintf("/api/v1/parts/%s/chapters/%d/image", p.ID, c.Idx)
					}
					vp.Chapters = append(vp.Chapters, ref)
				}
			}
			var stored []*model.Marker
			for _, m := range markers {
				if m.PartID == p.ID {
					stored = append(stored, m)
				}
			}
			for _, m := range partMarkers(stored, own, detection[r.LibraryID]) {
				m.StartMS += p.OffsetMS
				m.EndMS += p.OffsetMS
				vp.Markers = append(vp.Markers, m)
			}
			if t, ok := sheets[p.ID]; ok {
				vp.Trickplay = append(vp.Trickplay, PartTrickplay{PartID: p.ID, OffsetMS: p.OffsetMS, Trickplay: t})
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
					ID: f.ID, Codec: f.Codec, Language: deref(f.Language), Title: deref(f.Title), Default: f.IsDefault,
					Forced: f.Forced, HearingImpaired: f.HearingImpaired,
				})
			}
		}
		out[n] = vp
	}
	return out, nil
}

// markerDetection answers how each copy's library finds markers.
func (s *Store) markerDetection(ctx context.Context, versions []*model.Version) (map[uuid.UUID]domain.MarkerDetection, error) {
	libs := make([]uuid.UUID, len(versions))
	for n, v := range versions {
		libs[n] = v.LibraryID
	}
	rows, err := s.pool.Query(ctx, `SELECT id, markers FROM libraries WHERE id = ANY($1)`, libs)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]domain.MarkerDetection{}
	var lib uuid.UUID
	var detection domain.MarkerDetection
	_, err = pgx.ForEachRow(rows, []any{&lib, &detection}, func() error {
		out[lib] = detection
		return nil
	})
	return out, err
}

// partPreviews answers the idx of each part's chapters that have an image, and each part's
// trickplay sheets.
func (s *Store) partPreviews(ctx context.Context, parts []*model.Part) (map[uuid.UUID][]int, map[uuid.UUID]Trickplay, error) {
	ids := make([]uuid.UUID, len(parts))
	for n, p := range parts {
		ids[n] = p.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pv.part_id, pv.chapter_images, t.width, t.height, t.interval_ms, t.columns, t.rows, t.thumbnails
		FROM previews pv LEFT JOIN trickplay t ON t.part_id = pv.part_id WHERE pv.part_id = ANY($1)`, ids)
	if err != nil {
		return nil, nil, err
	}
	pictured, sheets := map[uuid.UUID][]int{}, map[uuid.UUID]Trickplay{}
	var part uuid.UUID
	var idx []int
	var w, h, interval, cols, rws, n *int
	_, err = pgx.ForEachRow(rows, []any{&part, &idx, &w, &h, &interval, &cols, &rws, &n}, func() error {
		pictured[part] = idx
		if n != nil {
			sheets[part] = sheetsOf(*w, *h, *interval, *cols, *rws, *n)
		}
		return nil
	})
	return pictured, sheets, err
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

// itemColumns are model.Item's, for a statement that reads whole items.
const itemColumns = `id, library_id, kind, title, sort_title, year, folder, added_at, parent_id, season_number,
	episode_number, episode_end, air_date, extra_kind, scan_title, original_title, overview, tagline, certificate,
	release_date, genres, studios, episode_order`

// itemColumnsOf is itemColumns read through alias, for a statement that joins items to others.
func itemColumnsOf(alias string) string {
	cols := strings.Split(itemColumns, ",")
	for n, c := range cols {
		cols[n] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// The columns of the other rows read whole.
const (
	versionColumns = `id, item_id, library_id, fingerprint, edition, label, container, width, height, video_codec,
		video_range, dv_profile, bitrate_kbps, size_bytes, duration_ms, missing_since`
	partColumns   = `id, version_id, idx, size_bytes, duration_ms, offset_ms`
	streamColumns = `part_id, idx, kind, codec, profile, language, title, is_default, forced, hearing_impaired,
		commentary, width, height, frame_rate, bit_depth, level, video_range, interlaced, dv_profile, dv_level,
		dv_compatibility, channels, channel_layout, sample_rate, bitrate_kbps`
	chapterColumns      = `part_id, idx, start_ms, end_ms, title`
	markerColumns       = `part_id, kind, source, start_ms, end_ms`
	subtitleFileColumns = `id, version_id, codec, language, title, forced, is_default, hearing_impaired`
)

// readRow answers the one row a statement finds, each column into the field of its name, or
// ErrNotFound.
func readRow[T any](ctx context.Context, q db, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return *new(T), err
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[T])
	return row, found(err)
}

// queryRows answers the rows a statement finds, each column into the field of its name.
func queryRows[T any](ctx context.Context, q db, sql string, args ...any) ([]*T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[T])
}

// ids is the items' ids, for an array parameter.
func ids(rows []*model.Item) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
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

func first(ids []uuid.UUID) uuid.UUID {
	if len(ids) == 0 {
		return uuid.UUID{}
	}
	return ids[0]
}
