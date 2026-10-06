package store

import (
	"cmp"
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"time"
	"uuid"

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
	Collections []TitleRef `json:"collections,omitzero"`
	// Credits are its cast and crew, as the highest-ranked source gives them.
	Credits []CreditRef `json:"credits,omitzero"`
	// Origin is who made a collection: an admin's is changed by hand, a provider's only by it.
	Origin domain.CollectionOrigin `json:"origin,omitzero"`
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

// ChapterRef is a chapter on the copy's whole timeline, across its parts. Image is the address of
// its picture, for those that have one, and SignedImage the same picture at an address a player
// that sends no token loads.
type ChapterRef struct {
	StartMS     int64  `json:"start_ms"`
	EndMS       int64  `json:"end_ms"`
	Title       string `json:"title,omitzero"`
	Image       string `json:"image,omitzero"`
	SignedImage string `json:"signed_image,omitzero"`
	// unsigned is the address SignedImage signs.
	unsigned string
}

// SignChapterImages gives each chapter's picture its signed address, as sign signs a path.
func (p *TitlePage) SignChapterImages(sign func(path string) string) {
	for _, v := range p.Versions {
		for c := range v.Chapters {
			if ref := &v.Chapters[c]; ref.unsigned != "" {
				ref.SignedImage = sign(ref.unsigned)
			}
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
	if err != nil {
		return TitlePage{}, found(err)
	}
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return TitlePage{}, cmp.Or(err, ErrNotFound)
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
		var origins map[model.UUID]domain.CollectionOrigin
		origins, err = s.origins(ctx, []*model.Item{item})
		p.Origin = origins[item.ID]
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

// visible reports whether a profile may see a title: a library it has, and a certificate within its
// age; a title it may not is not there to it.
func (s *Store) visible(ctx context.Context, profile, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = $1 AND sees(v, i))`,
		id.String(), profile.String()).Scan(&ok)
	return ok, err
}

// Visible answers those of titles a profile may see, in their order.
func (s *Store) Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	ids := make([]string, len(titles))
	for i, t := range titles {
		ids[i] = t.String()
	}
	return queryIDs(ctx, s.pool, `
		SELECT t.id::text FROM unnest($1::text[]::uuid[]) WITH ORDINALITY AS t(id, n)
		JOIN items i ON i.id = t.id, viewer($2) v
		WHERE sees(v, i) ORDER BY t.n`, ids, profile.String())
}

// HasLibrary reports whether a library is there and a profile may see its titles at all: as
// sees() asks, it has every library where none are listed for it.
func (s *Store) HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $2)
			AND (NOT EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1)
				OR EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1 AND library_id = $2))`,
		profile.String(), lib.String()).Scan(&ok)
	return ok, err
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
	v, pt, st, ch, sf, mk := s.q.Version, s.q.Part, s.q.Stream, s.q.Chapter, s.q.SubtitleFile, s.q.Marker
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
	markers, err := mk.WithContext(ctx).Where(mk.PartID.In(pids...)).Find()
	if err != nil {
		return nil, err
	}
	subs, err := sf.WithContext(ctx).Where(sf.VersionID.In(vids...)).Order(sf.RelPath).Find()
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
			ID: uuid.UUID(r.ID), Edition: deref(r.Edition), Label: deref(r.Label), Container: r.Container,
			DurationMS: r.DurationMS, SizeBytes: r.SizeBytes, BitrateKbps: r.BitrateKbps,
			Parts: len(byVersion[r.ID]), MissingSince: r.MissingSince, Streams: []StreamPage{},
		}
		for k, p := range byVersion[r.ID] {
			vp.Files = append(vp.Files, PartRef{
				ID: uuid.UUID(p.ID), Index: int(p.Idx), SizeBytes: p.SizeBytes, DurationMS: p.DurationMS, OffsetMS: p.OffsetMS,
			})
			var own []*model.Chapter
			for _, c := range chapters {
				if c.PartID == p.ID {
					own = append(own, c)
					ref := ChapterRef{StartMS: p.OffsetMS + c.StartMS, EndMS: p.OffsetMS + c.EndMS, Title: deref(c.Title)}
					if slices.Contains(pictured[p.ID], c.Idx) {
						ref.Image = fmt.Sprintf("/api/v1/parts/%s/chapters/%d/image", uuid.UUID(p.ID), c.Idx)
						ref.unsigned = fmt.Sprintf("/api/v1/parts/%s/chapter-images/%d", uuid.UUID(p.ID), c.Idx)
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
				vp.Trickplay = append(vp.Trickplay, PartTrickplay{PartID: uuid.UUID(p.ID), OffsetMS: p.OffsetMS, Trickplay: t})
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

// markerDetection answers how each copy's library finds markers.
func (s *Store) markerDetection(ctx context.Context, versions []*model.Version) (map[model.UUID]domain.MarkerDetection, error) {
	ids := make([]driver.Valuer, len(versions))
	for n, v := range versions {
		ids[n] = v.LibraryID
	}
	l := s.q.Library
	libs, err := l.WithContext(ctx).Select(l.ID, l.Markers).Where(l.ID.In(ids...)).Find()
	out := map[model.UUID]domain.MarkerDetection{}
	for _, lib := range libs {
		out[lib.ID] = lib.Markers
	}
	return out, err
}

// partPreviews answers the idx of each part's chapters that have an image, and each part's
// trickplay sheets.
func (s *Store) partPreviews(ctx context.Context, parts []*model.Part) (map[model.UUID][]int, map[model.UUID]Trickplay, error) {
	ids := make([]string, len(parts))
	for n, p := range parts {
		ids[n] = uuid.UUID(p.ID).String()
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pv.part_id::text, pv.chapter_images, t.width, t.height, t.interval_ms, t.columns, t.rows, t.thumbnails
		FROM previews pv LEFT JOIN trickplay t ON t.part_id = pv.part_id WHERE pv.part_id::text = ANY($1)`, ids)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	pictured, sheets := map[model.UUID][]int{}, map[model.UUID]Trickplay{}
	for rows.Next() {
		var id string
		var idx []int
		var w, h, interval, cols, rws, n *int
		if err := rows.Scan(&id, &idx, &w, &h, &interval, &cols, &rws, &n); err != nil {
			return nil, nil, err
		}
		part, err := uuid.Parse(id)
		if err != nil {
			return nil, nil, err
		}
		pictured[model.UUID(part)] = idx
		if n != nil {
			sheets[model.UUID(part)] = sheetsOf(*w, *h, *interval, *cols, *rws, *n)
		}
	}
	return pictured, sheets, rows.Err()
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
