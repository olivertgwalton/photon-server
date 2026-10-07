package store

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// TitlePage is everything a title's page shows, read in one go: a film's or episode's versions, a
// show's seasons, a season's episodes, and whatever extras and videos the title has.
type TitlePage struct {
	ID            uuid.UUID
	Kind          domain.ItemKind
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Certificate   string
	Year          int
	ReleaseDate   domain.Date
	Genres        []string
	Studios       []string
	IDs           map[domain.Provider]string
	Ratings       []domain.Rating
	// Collections are the box sets it is in.
	Collections []CollectionCard
	// Credits are its cast and crew, as the highest-ranked source gives them.
	Credits []CreditRef
	// Origin is who made a collection: an admin's is changed by hand, a provider's only by it.
	Origin    domain.CollectionOrigin
	Placement domain.CollectionPlacement
	// Rule is what finds a smart collection's titles.
	Rule *SmartRule
	// EpisodeOrder is the order a show's episode files are numbered in.
	EpisodeOrder domain.EpisodeOrder
	// Locale is a film's or show's own metadata language and certification country, over its
	// library's; empty where it takes its library's.
	Locale        domain.Locale
	AddedAt       time.Time
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	Show          *TitleRef
	Season        *TitleRef
	Versions      []VersionPage
	Seasons       []SeasonCard
	Episodes      []EpisodeCard
	Extras        []ExtraCard
	Videos        []VideoLink
	// State is what the profile asking has made of it.
	State TitleState
	// Artwork is the title's pictures by kind, best first, by id: /api/v1/artwork/{id}.
	Artwork map[domain.ArtworkKind][]uuid.UUID
	// Blurhashes are those of its pictures that have one, by id.
	Blurhashes Blurhashes
	// Themes are the tunes to play under its page, in order, by id: /api/v1/themes/{id}. A season's
	// and an episode's are its show's.
	Themes []uuid.UUID
}

type TitleRef struct {
	ID    uuid.UUID
	Title string
}

// VersionPage is one copy: what it is, the tracks in it and the subtitles beside it. A copy whose
// files are gone says since when.
type VersionPage struct {
	ID           uuid.UUID
	Edition      string
	Label        string
	Container    string
	DurationMS   int64
	SizeBytes    int64
	BitrateKbps  int
	Parts        int
	MissingSince *time.Time
	Streams      []StreamPage
	Subtitles    []SubtitleRef
	Chapters     []ChapterRef
	Markers      []MarkerRef
	// Files are its parts in order, each where it starts on the copy's timeline, by the id the
	// /api/v1/parts/{id} routes take.
	Files []PartRef
	// Trickplay is the thumbnail sheets of each part that has them; a part's sheets are at
	// /api/v1/parts/{part_id}/trickplay/{n}.
	Trickplay []PartTrickplay
	// DefaultAudioStream and DefaultSubtitleStream or DefaultSubtitleFile are the tracks it plays
	// with unasked, for the profile asking: none where no subtitle comes on.
	DefaultAudioStream    *int
	DefaultSubtitleStream *int
	DefaultSubtitleFile   *uuid.UUID
}

// PartRef is one file of a copy.
type PartRef struct {
	ID uuid.UUID
	// File is the name of the part's file, without the folders it is in.
	File       string
	Index      int
	SizeBytes  int64
	DurationMS int64
	OffsetMS   int64
}

// PartTrickplay is a part's thumbnail sheets, its thumbnails timed from OffsetMS on the copy's
// timeline.
type PartTrickplay struct {
	PartID   uuid.UUID
	OffsetMS int64
	Trickplay
}

// StreamPage is a track of a copy's first part; the parts of one copy are cut from one master.
type StreamPage struct {
	Index           int
	Kind            domain.StreamKind
	Codec           string
	Profile         string
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
	Commentary      bool
	Width           int
	Height          int
	FrameRate       float64
	BitDepth        int16
	Level           int
	Range           domain.Range
	DVProfile       int16
	Channels        int
	ChannelLayout   string
	SampleRate      int
	BitrateKbps     int
}

type SubtitleRef struct {
	ID              uuid.UUID
	Codec           string
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
}

// ChapterRef is a chapter on the copy's whole timeline, across its parts. Image is the address of
// its picture, for those that have one.
type ChapterRef struct {
	StartMS int64
	EndMS   int64
	Title   string
	Image   string
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
	ID       uuid.UUID
	Number   int
	Title    string
	Overview string
	Year     int
	Aired    domain.Date
	Episodes int
	Poster   uuid.UUID
	State    TitleState
	// Blurhashes are those of its pictures that have one, by id, as on every card.
	Blurhashes Blurhashes
}

type EpisodeCard struct {
	ID         uuid.UUID
	Number     *int
	End        *int
	Title      string
	Overview   string
	Aired      domain.Date
	DurationMS int64
	Thumb      uuid.UUID
	State      TitleState
	Blurhashes Blurhashes
}

// ExtraCard is a trailer or other extra, pictured by a still of its video where its previews are
// made.
type ExtraCard struct {
	ID         uuid.UUID
	Kind       domain.ExtraKind
	Title      string
	DurationMS int64
	Image      string
}

type CollectionCard struct {
	ID         uuid.UUID
	Title      string
	Poster     uuid.UUID
	Blurhashes Blurhashes
}

// VideoLink is a provider's link to a video hosted elsewhere, with its site's still of it where
// the site publishes one, served at /api/v1/artwork/{thumb}.
type VideoLink struct {
	Kind      domain.ExtraKind
	Site      string
	Key       string
	Name      string
	Language  string
	Published *time.Time
	Thumb     uuid.UUID
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
	p.Ratings = ratings[item.ID]
	if p.Collections, err = s.collectionsOf(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Credits, err = s.credits(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Show != nil {
		show, err := s.credits(ctx, p.Show.ID)
		if err != nil {
			return TitlePage{}, err
		}
		p.Credits = billed(show, p.Credits)
	}
	if item.Kind == domain.ItemMovie || item.Kind == domain.ItemShow {
		err := s.pool.QueryRow(ctx, `
			SELECT coalesce(metadata_language, ''), coalesce(certification_country, '') FROM items WHERE id = $1`, id).
			Scan(&p.Locale.Language, &p.Locale.Country)
		if err != nil {
			return TitlePage{}, err
		}
	}
	switch item.Kind {
	case domain.ItemShow:
		p.EpisodeOrder = item.EpisodeOrder
		p.Seasons, err = s.seasons(ctx, profile, item.ID)
	case domain.ItemSeason:
		p.Episodes, err = s.episodes(ctx, profile, item.ID)
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		var versions map[uuid.UUID][]VersionPage
		versions, err = s.Versions(ctx, []uuid.UUID{item.ID})
		p.Versions = versions[item.ID]
	case domain.ItemCollection:
		err = s.pool.QueryRow(ctx, `SELECT origin, placement, rule FROM collections WHERE item_id = $1`, item.ID).
			Scan(&p.Origin, &p.Placement, &p.Rule)
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
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{item})
	return ids[item], err
}

// ExternalIDs answers each title's ids at the providers that have it; a title with none is left out.
func (s *Store) ExternalIDs(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]map[domain.Provider]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, provider, value FROM external_ids WHERE item_id = ANY($1)`, items)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]map[domain.Provider]string{}
	var item uuid.UUID
	var provider domain.Provider
	var value string
	_, err = pgx.ForEachRow(rows, []any{&item, &provider, &value}, func() error {
		if out[item] == nil {
			out[item] = map[domain.Provider]string{}
		}
		out[item][provider] = value
		return nil
	})
	return out, err
}

// Seasons answers a show's seasons, ErrNotFound for a show the profile may not see.
func (s *Store) Seasons(ctx context.Context, profile, show uuid.UUID) ([]SeasonCard, error) {
	if ok, err := s.visible(ctx, profile, show); err != nil || !ok {
		return nil, cmp.Or(err, ErrNotFound)
	}
	return s.seasons(ctx, profile, show)
}

// Episodes answers the episodes of a show, every season's, or of one season, in order, as cards;
// ErrNotFound for one the profile may not see.
func (s *Store) Episodes(ctx context.Context, profile, of uuid.UUID) ([]Card, error) {
	if ok, err := s.visible(ctx, profile, of); err != nil || !ok {
		return nil, cmp.Or(err, ErrNotFound)
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumnsOf("e")+` FROM items e JOIN items season ON season.id = e.parent_id
		WHERE e.kind = 'episode' AND (season.id = $1 OR season.parent_id = $1)
		ORDER BY e.season_number, e.episode_number, e.air_date, e.sort_title`, of)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
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
			Overview: deref(r.Overview), Aired: domain.Date(deref(aired)), DurationMS: lengths[r.ID].ms,
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
		out[n] = ExtraCard{ID: r.ID, Kind: deref(r.ExtraKind), Title: r.Title, DurationMS: lengths[r.ID].ms}
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

// onDisk is what of a title is on disk: how long its longest copy runs, and how many copies.
type onDisk struct {
	ms       int64
	versions int
}

// durations answers what of each title is on disk.
func (s *Store) durations(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]onDisk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, max(duration_ms), count(*) FROM versions WHERE item_id = ANY($1) AND missing_since IS NULL
		GROUP BY item_id`, items)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]onDisk{}
	var item uuid.UUID
	var held onDisk
	_, err = pgx.ForEachRow(rows, []any{&item, &held.ms, &held.versions}, func() error {
		out[item] = held
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
// Versions answers the copies of each of items, those on disk first, the longest first among them;
// a title with none is left out. However many titles, it is the same few queries.
func (s *Store) Versions(ctx context.Context, items []uuid.UUID) (map[uuid.UUID][]VersionPage, error) {
	rows, err := queryRows[model.Version](ctx, s.pool, `SELECT `+versionColumns+` FROM versions WHERE item_id = ANY($1)`, items)
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
	files := map[uuid.UUID]string{}
	named, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (part_id) part_id, rel_path FROM part_files WHERE part_id = ANY($1) ORDER BY part_id, rel_path`, pids)
	if err != nil {
		return nil, err
	}
	var part uuid.UUID
	var rel string
	if _, err := pgx.ForEachRow(named, []any{&part, &rel}, func() error {
		files[part] = path.Base(rel)
		return nil
	}); err != nil {
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
	out := make(map[uuid.UUID][]VersionPage, len(items))
	for _, r := range rows {
		vp := VersionPage{
			ID: r.ID, Edition: deref(r.Edition), Label: deref(r.Label), Container: r.Container,
			DurationMS: r.DurationMS, SizeBytes: r.SizeBytes, BitrateKbps: r.BitrateKbps,
			Parts: len(byVersion[r.ID]), MissingSince: r.MissingSince, Streams: []StreamPage{},
		}
		for k, p := range byVersion[r.ID] {
			vp.Files = append(vp.Files, PartRef{
				ID: p.ID, File: files[p.ID], Index: int(p.Idx), SizeBytes: p.SizeBytes, DurationMS: p.DurationMS, OffsetMS: p.OffsetMS,
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
		out[r.ItemID] = append(out[r.ItemID], vp)
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
