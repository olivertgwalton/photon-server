package store

import (
	"context"
	"errors"
	"uuid"

	"golang.org/x/text/language"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Preferences answers how a profile plays: the defaults, for one that never changed them.
func (s *Store) Preferences(ctx context.Context, profile uuid.UUID) (domain.Preferences, error) {
	var out domain.Preferences
	var audio, subtitles string
	err := s.pool.QueryRow(ctx, `
		SELECT audio_language, audio_track, subtitle_language, subtitle_mode, remember_audio, remember_subtitles,
			max_bitrate_kbps, next_episode, intro_action, credits_action, theme_music, saved_at
		FROM profile_preferences WHERE profile_id = $1`, profile).Scan(
		&audio, &out.AudioTrack, &subtitles, &out.SubtitleMode, &out.RememberAudio, &out.RememberSubtitles,
		&out.MaxBitrateKbps, &out.NextEpisode, &out.IntroAction, &out.CreditsAction, &out.ThemeMusic, &out.SavedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultPreferences(), nil
	}
	if err != nil {
		return domain.Preferences{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT home_row, visibility FROM home_sections WHERE profile_id = $1 ORDER BY position`, profile)
	if err != nil {
		return domain.Preferences{}, err
	}
	home, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.HomeSection])
	if err != nil {
		return domain.Preferences{}, err
	}
	out.AudioLanguage, _ = language.Parse(audio)
	out.SubtitleLanguage, _ = language.Parse(subtitles)
	out.Home = domain.ArrangeHome(home)
	return out, nil
}

// SetPreferences keeps how a profile plays, answering them as kept. ErrNotFound for no profile.
func (s *Store) SetPreferences(ctx context.Context, profile uuid.UUID, p domain.Preferences) (domain.Preferences, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO profile_preferences (profile_id, audio_language, audio_track, subtitle_language, subtitle_mode,
				remember_audio, remember_subtitles, max_bitrate_kbps, next_episode, intro_action, credits_action,
				theme_music, saved_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
			ON CONFLICT (profile_id) DO UPDATE SET
				audio_language = excluded.audio_language, audio_track = excluded.audio_track,
				subtitle_language = excluded.subtitle_language, subtitle_mode = excluded.subtitle_mode,
				remember_audio = excluded.remember_audio, remember_subtitles = excluded.remember_subtitles,
				max_bitrate_kbps = excluded.max_bitrate_kbps, next_episode = excluded.next_episode,
				intro_action = excluded.intro_action, credits_action = excluded.credits_action,
				theme_music = excluded.theme_music, saved_at = excluded.saved_at`,
			profile, languageText(p.AudioLanguage), p.AudioTrack, languageText(p.SubtitleLanguage), p.SubtitleMode,
			p.RememberAudio, p.RememberSubtitles, p.MaxBitrateKbps, p.NextEpisode, p.IntroAction, p.CreditsAction,
			p.ThemeMusic)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM home_sections WHERE profile_id = $1`, profile); err != nil {
			return err
		}
		b := &pgx.Batch{}
		for i, h := range domain.ArrangeHome(p.Home) {
			b.Queue(`INSERT INTO home_sections (profile_id, home_row, position, visibility) VALUES ($1, $2, $3, $4)`,
				profile, h.Row, i, h.Visibility)
		}
		return tx.SendBatch(ctx, b).Close()
	})
	if violates(err, foreignKeyViolation) {
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
	var out domain.ChosenTracks
	err := s.pool.QueryRow(ctx, `
		SELECT audio_stream, subtitle_stream, subtitle_file FROM watch_state WHERE profile_id = $1 AND item_id = $2`,
		profile, item).Scan(&out.Audio, &out.Subtitle, &out.SubtitleFile)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ChosenTracks{}, nil
	}
	return out, err
}

// ChooseTracks keeps the tracks a player says it is playing a film or episode with. A sound or
// subtitle it does not say stays as it was; one subtitle said replaces the other.
func (s *Store) ChooseTracks(ctx context.Context, profile, item uuid.UUID, t domain.ChosenTracks) error {
	if t.Audio == nil && t.Subtitle == nil && t.SubtitleFile == nil {
		return nil
	}
	subtitles := t.Subtitle != nil || t.SubtitleFile != nil
	_, err := s.pool.Exec(ctx, `
		INSERT INTO watch_state (profile_id, item_id, audio_stream, subtitle_stream, subtitle_file)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (profile_id, item_id) DO UPDATE SET
			audio_stream = coalesce(excluded.audio_stream, watch_state.audio_stream),
			subtitle_stream = CASE WHEN $6 THEN excluded.subtitle_stream ELSE watch_state.subtitle_stream END,
			subtitle_file = CASE WHEN $6 THEN excluded.subtitle_file ELSE watch_state.subtitle_file END`,
		profile, item, t.Audio, t.Subtitle, t.SubtitleFile, subtitles)
	return err
}
