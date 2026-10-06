package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Preferences answers how a profile plays: the defaults, for one that never changed them.
func (s *Store) Preferences(ctx context.Context, profile uuid.UUID) (domain.Preferences, error) {
	pp, hs := s.q.ProfilePreference, s.q.HomeSection
	row, err := pp.WithContext(ctx).Where(pp.ProfileID.Eq(model.UUID(profile))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.DefaultPreferences(), nil
	}
	if err != nil {
		return domain.Preferences{}, err
	}
	sections, err := hs.WithContext(ctx).Where(hs.ProfileID.Eq(model.UUID(profile))).Order(hs.Position).Find()
	if err != nil {
		return domain.Preferences{}, err
	}
	home := make([]domain.HomeSection, len(sections))
	for i, h := range sections {
		home[i] = domain.HomeSection{Row: h.HomeRow, Visibility: h.Visibility}
	}
	audio, _ := language.Parse(row.AudioLanguage)
	subtitles, _ := language.Parse(row.SubtitleLanguage)
	return domain.Preferences{
		AudioLanguage: audio, AudioTrack: row.AudioTrack,
		SubtitleLanguage: subtitles, SubtitleMode: row.SubtitleMode,
		RememberAudio: row.RememberAudio, RememberSubtitles: row.RememberSubtitles,
		MaxBitrateKbps: int(row.MaxBitrateKbps), NextEpisode: row.NextEpisode,
		IntroAction: row.IntroAction, CreditsAction: row.CreditsAction, ThemeMusic: row.ThemeMusic,
		Home: domain.ArrangeHome(home), SavedAt: row.SavedAt,
	}, nil
}

// SetPreferences keeps how a profile plays, answering them as kept. ErrNotFound for no profile.
func (s *Store) SetPreferences(ctx context.Context, profile uuid.UUID, p domain.Preferences) (domain.Preferences, error) {
	row := model.ProfilePreference{
		ProfileID: model.UUID(profile), AudioLanguage: languageText(p.AudioLanguage), AudioTrack: p.AudioTrack,
		SubtitleLanguage: languageText(p.SubtitleLanguage), SubtitleMode: p.SubtitleMode,
		RememberAudio: p.RememberAudio, RememberSubtitles: p.RememberSubtitles,
		MaxBitrateKbps: int32(p.MaxBitrateKbps), NextEpisode: p.NextEpisode,
		IntroAction: p.IntroAction, CreditsAction: p.CreditsAction, ThemeMusic: p.ThemeMusic, SavedAt: time.Now(),
	}
	err := s.q.Transaction(func(tx *query.Query) error {
		if err := tx.ProfilePreference.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row); err != nil {
			return err
		}
		hs := tx.HomeSection
		if _, err := hs.WithContext(ctx).Where(hs.ProfileID.Eq(row.ProfileID)).Delete(); err != nil {
			return err
		}
		home := domain.ArrangeHome(p.Home)
		sections := make([]*model.HomeSection, len(home))
		for i, h := range home {
			sections[i] = &model.HomeSection{ProfileID: row.ProfileID, HomeRow: h.Row, Position: int16(i), Visibility: h.Visibility}
		}
		return hs.WithContext(ctx).Create(sections...)
	})
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return domain.Preferences{}, ErrNotFound
	}
	if err != nil {
		return domain.Preferences{}, err
	}
	return s.Preferences(ctx, profile)
}

// languageText is a language as kept: "" for any.
func languageText(t language.Tag) string {
	if t == language.Und {
		return ""
	}
	return t.String()
}

// ChosenTracks answers the tracks a profile last chose for a film or episode.
func (s *Store) ChosenTracks(ctx context.Context, profile, item uuid.UUID) (domain.ChosenTracks, error) {
	w := s.q.WatchState
	row, err := w.WithContext(ctx).Where(w.ProfileID.Eq(model.UUID(profile)), w.ItemID.Eq(model.UUID(item))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ChosenTracks{}, nil
	}
	if err != nil {
		return domain.ChosenTracks{}, err
	}
	var out domain.ChosenTracks
	if row.AudioStream != nil {
		out.Audio = new(int(*row.AudioStream))
	}
	if row.SubtitleStream != nil {
		out.Subtitle = new(int(*row.SubtitleStream))
	}
	if row.SubtitleFile != nil {
		out.SubtitleFile = new(uuid.UUID(*row.SubtitleFile))
	}
	return out, nil
}

// ChooseTracks keeps the tracks a player says it is playing a film or episode with. A sound or
// subtitle it does not say stays as it was; one subtitle said replaces the other.
func (s *Store) ChooseTracks(ctx context.Context, profile, item uuid.UUID, t domain.ChosenTracks) error {
	if t.Audio == nil && t.Subtitle == nil && t.SubtitleFile == nil {
		return nil
	}
	subtitles := t.Subtitle != nil || t.SubtitleFile != nil
	var file *string
	if t.SubtitleFile != nil {
		file = new(t.SubtitleFile.String())
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO watch_state (profile_id, item_id, audio_stream, subtitle_stream, subtitle_file)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (profile_id, item_id) DO UPDATE SET
			audio_stream = coalesce(excluded.audio_stream, watch_state.audio_stream),
			subtitle_stream = CASE WHEN $6 THEN excluded.subtitle_stream ELSE watch_state.subtitle_stream END,
			subtitle_file = CASE WHEN $6 THEN excluded.subtitle_file ELSE watch_state.subtitle_file END`,
		profile.String(), item.String(), t.Audio, t.Subtitle, file, subtitles)
	return err
}
