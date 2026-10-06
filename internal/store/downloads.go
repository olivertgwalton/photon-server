package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Download is a profile's download of a part: the part's own file where Quality is nil, ready as
// it is, else its conversion to that quality, as far as it has got.
type Download struct {
	ID uuid.UUID
	// Device is the session of the device that asked for it.
	Device     uuid.UUID
	Item       uuid.UUID
	Part       uuid.UUID
	Quality    *domain.Quality
	Conversion uuid.UUID
	State      domain.DownloadState
	Progress   float64
	SizeBytes  int64
	Error      string
	Created    time.Time
}

// AddDownload records a profile's download of a part of a title on a device, converted to a
// quality unless q is nil. The conversion is shared with every download of the part at that
// quality and video and queued where it is new or last failed. A download the device asks for again is
// answered as it stands.
func (s *Store) AddDownload(ctx context.Context, profile, device, item, part uuid.UUID, q *domain.Quality) (Download, error) {
	var added uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var conversion *uuid.UUID
		if q != nil {
			var id uuid.UUID
			var state domain.DownloadState
			if err := tx.QueryRow(ctx, `
				INSERT INTO conversions (part_id, max_bitrate_kbps, max_width, video_codec, video_range) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (part_id, max_bitrate_kbps, max_width, video_codec, video_range) DO UPDATE SET
					state = CASE conversions.state WHEN 'failed' THEN 'queued' ELSE conversions.state END,
					progress = CASE conversions.state WHEN 'failed' THEN 0 ELSE conversions.progress END,
					error = CASE conversions.state WHEN 'failed' THEN NULL ELSE conversions.error END,
					finished_at = CASE conversions.state WHEN 'failed' THEN NULL ELSE conversions.finished_at END
				RETURNING id, state`, part, q.MaxBitrateKbps, q.MaxWidth, q.Codec, q.Range).Scan(&id, &state); err != nil {
				return err
			}
			if state == domain.DownloadQueued {
				if err := enqueue(ctx, tx, domain.JobConvert, id); err != nil {
					return err
				}
			}
			conversion = &id
		}
		return tx.QueryRow(ctx, `
			INSERT INTO downloads (profile_id, session_id, item_id, part_id, conversion_id) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (profile_id, session_id, part_id, conversion_id) DO UPDATE SET item_id = excluded.item_id
			RETURNING id`, profile, device, item, part, conversion).Scan(&added)
	})
	if err != nil {
		return Download{}, err
	}
	return s.Download(ctx, profile, added)
}

// downloadRow is a download read with its part and conversion.
type downloadRow struct {
	ID             uuid.UUID
	SessionID      uuid.UUID
	ItemID         uuid.UUID
	PartID         uuid.UUID
	CreatedAt      time.Time
	ConversionID   *uuid.UUID
	MaxBitrateKbps int
	MaxWidth       int
	VideoCodec     domain.VideoCodec
	VideoRange     domain.Range
	State          domain.DownloadState
	Progress       float64
	SizeBytes      int64
	Error          string
}

// downloads reads a profile's downloads, the newest first: those of a device where device is set,
// the one with an id where id is.
func (s *Store) downloads(ctx context.Context, profile uuid.UUID, device, id *uuid.UUID) ([]Download, error) {
	sql := `
		SELECT d.id, d.session_id, d.item_id, d.part_id, d.created_at, d.conversion_id,
			coalesce(c.max_bitrate_kbps, 0) AS max_bitrate_kbps, coalesce(c.max_width, 0) AS max_width,
			coalesce(c.video_codec, '') AS video_codec, coalesce(c.video_range, '') AS video_range,
			coalesce(c.state, 'ready') AS state, coalesce(c.progress, 1) AS progress,
			coalesce(c.size_bytes, CASE WHEN c.id IS NULL THEN p.size_bytes END, 0) AS size_bytes,
			coalesce(c.error, '') AS error
		FROM downloads d JOIN parts p ON p.id = d.part_id LEFT JOIN conversions c ON c.id = d.conversion_id
		WHERE d.profile_id = $1 AND ($2::uuid IS NULL OR d.session_id = $2) AND ($3::uuid IS NULL OR d.id = $3)
		ORDER BY d.created_at DESC, d.id`
	found, err := s.pool.Query(ctx, sql, profile, device, id)
	if err != nil {
		return nil, err
	}
	rows, err := pgx.CollectRows(found, pgx.RowToStructByName[downloadRow])
	if err != nil {
		return nil, err
	}
	out := make([]Download, len(rows))
	for n, r := range rows {
		out[n] = Download{
			ID: r.ID, Device: r.SessionID, Item: r.ItemID, Part: r.PartID, State: r.State,
			Progress: r.Progress, SizeBytes: r.SizeBytes, Error: r.Error, Created: r.CreatedAt,
		}
		if r.ConversionID != nil {
			out[n].Conversion = *r.ConversionID
			out[n].Quality = &domain.Quality{MaxBitrateKbps: r.MaxBitrateKbps, MaxWidth: r.MaxWidth, Codec: r.VideoCodec, Range: r.VideoRange}
		}
	}
	return out, nil
}

// Downloads answers a profile's downloads, the newest first: every one, or with device set only
// that device's.
func (s *Store) Downloads(ctx context.Context, profile uuid.UUID, device *uuid.UUID) ([]Download, error) {
	return s.downloads(ctx, profile, device, nil)
}

// Download answers one of a profile's downloads. ErrNotFound for none of that id of the profile's.
func (s *Store) Download(ctx context.Context, profile, id uuid.UUID) (Download, error) {
	d, err := s.downloads(ctx, profile, nil, &id)
	if err != nil {
		return Download{}, err
	}
	if len(d) == 0 {
		return Download{}, ErrNotFound
	}
	return d[0], nil
}

// RemoveDownload forgets one of a profile's downloads, and its conversion where no other download
// needs it. ErrNotFound for none of that id of the profile's.
func (s *Store) RemoveDownload(ctx context.Context, profile, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `DELETE FROM downloads WHERE id = $1 AND profile_id = $2`, id, profile)); err != nil {
			return err
		}
		return dropUnwanted(ctx, tx)
	})
}

// ExpireDownloads forgets every download that has been ready, or failed, since before a time, and
// the conversions no download needs any more. It answers how many downloads.
func (s *Store) ExpireDownloads(ctx context.Context, before time.Time) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `
			DELETE FROM downloads d WHERE d.created_at < $1 AND NOT EXISTS (
				SELECT 1 FROM conversions c WHERE c.id = d.conversion_id
					AND (c.state IN ('queued', 'converting') OR c.finished_at >= $1))`, before)
		if err != nil {
			return err
		}
		n = res.RowsAffected()
		return dropUnwanted(ctx, tx)
	})
	return n, err
}

// dropUnwanted removes the conversions no download needs, and their jobs not yet run.
func dropUnwanted(ctx context.Context, tx db) error {
	if _, err := tx.Exec(ctx, `DELETE FROM conversions c WHERE NOT EXISTS (SELECT 1 FROM downloads d WHERE d.conversion_id = c.id)`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM jobs j WHERE j.kind = 'convert' AND j.state IN ('queued', 'dead')
			AND NOT EXISTS (SELECT 1 FROM conversions c WHERE c.id = j.subject)`)
	return err
}

// Conversion is what converting a part needs: the part, the quality, and its copy as deciding how
// it plays needs it.
type Conversion struct {
	Part        uuid.UUID
	Quality     domain.Quality
	Container   string
	BitrateKbps int
	Duration    time.Duration
	Streams     []domain.Stream
}

// StartConversion records that a node is converting a part, and answers what it needs to.
// ErrNotFound for a conversion that is no longer wanted, or made or failed already.
func (s *Store) StartConversion(ctx context.Context, id, node uuid.UUID) (Conversion, error) {
	var out Conversion
	var durationMS int64
	err := s.pool.QueryRow(ctx, `
		UPDATE conversions c SET state = 'converting', node_id = $2, progress = 0
		FROM parts p JOIN versions v ON v.id = p.version_id
		WHERE c.id = $1 AND p.id = c.part_id AND c.state IN ('queued', 'converting')
			-- A device signed out takes its downloads with it, leaving their conversions to the sweep.
			AND EXISTS (SELECT 1 FROM downloads d WHERE d.conversion_id = c.id)
		RETURNING c.part_id, c.max_bitrate_kbps, c.max_width, c.video_codec, c.video_range, v.container, v.bitrate_kbps,
			p.duration_ms`, id, node).Scan(&out.Part, &out.Quality.MaxBitrateKbps, &out.Quality.MaxWidth, &out.Quality.Codec,
		&out.Quality.Range, &out.Container, &out.BitrateKbps, &durationMS)
	if err != nil {
		return Conversion{}, found(err)
	}
	out.Duration = time.Duration(durationMS) * time.Millisecond
	streams, err := queryRows[model.Stream](ctx, s.pool, `SELECT `+streamColumns+` FROM streams WHERE part_id = $1 ORDER BY idx`, out.Part)
	if err != nil {
		return Conversion{}, err
	}
	for _, t := range streams {
		out.Streams = append(out.Streams, mediaStream(t))
	}
	return out, nil
}

// converting updates a conversion a node is still making, setting what set says from $3 on.
// ErrNotFound where it is no longer wanted, or another node has taken it.
func (s *Store) converting(ctx context.Context, id, node uuid.UUID, set string, args ...any) error {
	return affected(s.pool.Exec(ctx, `UPDATE conversions SET `+set+` WHERE id = $1 AND node_id = $2 AND state = 'converting'`,
		append([]any{id, node}, args...)...))
}

// ConversionProgress records how far a conversion has got, from 0 to 1.
func (s *Store) ConversionProgress(ctx context.Context, id, node uuid.UUID, progress float64) error {
	return s.converting(ctx, id, node, `progress = $3`, progress)
}

// FinishConversion records a conversion made, and its file's size.
func (s *Store) FinishConversion(ctx context.Context, id, node uuid.UUID, size int64) error {
	return s.converting(ctx, id, node, `state = 'ready', progress = 1, size_bytes = $3, finished_at = $4`, size, time.Now())
}

// FailConversion records why a conversion could not be made.
func (s *Store) FailConversion(ctx context.Context, id, node uuid.UUID, reason string) error {
	return s.converting(ctx, id, node, `state = 'failed', error = $3, finished_at = $4`, reason, time.Now())
}

// RequeueConversion puts a conversion a node had to stop back in the queue, to be made from the
// beginning.
func (s *Store) RequeueConversion(ctx context.Context, id, node uuid.UUID) error {
	return s.converting(ctx, id, node, `state = 'queued', progress = 0, node_id = NULL`)
}

// ConversionsOn answers the conversions whose files a node is making or holds.
func (s *Store) ConversionsOn(ctx context.Context, node uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM conversions WHERE node_id = $1 AND state IN ('converting', 'ready')`, node)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ConvertedFile answers a download's conversion and the node holding its file. ErrNotFound for no
// such download, or one whose conversion is not ready.
func (s *Store) ConvertedFile(ctx context.Context, download uuid.UUID) (conversion, node uuid.UUID, err error) {
	var holder *uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT c.id, c.node_id FROM downloads d JOIN conversions c ON c.id = d.conversion_id
		WHERE d.id = $1 AND c.state = 'ready' LIMIT 1`, download).Scan(&conversion, &holder)
	if err == nil && holder == nil {
		err = ErrNotFound
	}
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, found(err)
	}
	return conversion, *holder, nil
}
