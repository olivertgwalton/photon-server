package store

import (
	"context"
	"time"
	"uuid"

	"gorm.io/gen/field"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Download is a profile's download of a part: the part's own file where Quality is nil, ready as
// it is, else its conversion to that quality, as far as it has got.
type Download struct {
	ID         uuid.UUID
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

// AddDownload records a profile's download of a part of a title, converted to a quality unless q
// is nil. The conversion is shared with every profile that asks for the part at that quality and
// queued where it is new or last failed. A download asked for again is answered as it stands.
func (s *Store) AddDownload(ctx context.Context, profile, item, part uuid.UUID, q *domain.Quality) (Download, error) {
	var added struct{ ID model.UUID }
	err := s.q.Transaction(func(tx *query.Query) error {
		db := tx.Download.WithContext(ctx).UnderlyingDB()
		var conversion *model.UUID
		if q != nil {
			var c struct {
				ID    model.UUID
				State domain.DownloadState
			}
			if err := db.Raw(`
				INSERT INTO conversions (part_id, max_bitrate_kbps, max_width) VALUES (?, ?, ?)
				ON CONFLICT (part_id, max_bitrate_kbps, max_width) DO UPDATE SET
					state = CASE conversions.state WHEN 'failed' THEN 'queued' ELSE conversions.state END,
					progress = CASE conversions.state WHEN 'failed' THEN 0 ELSE conversions.progress END,
					error = CASE conversions.state WHEN 'failed' THEN NULL ELSE conversions.error END,
					finished_at = CASE conversions.state WHEN 'failed' THEN NULL ELSE conversions.finished_at END
				RETURNING id, state`, part.String(), q.MaxBitrateKbps, q.MaxWidth).Scan(&c).Error; err != nil {
				return err
			}
			if c.State == domain.DownloadQueued {
				if err := enqueue(ctx, tx, domain.JobConvert, c.ID); err != nil {
					return err
				}
			}
			conversion = &c.ID
		}
		return db.Raw(`
			INSERT INTO downloads (profile_id, item_id, part_id, conversion_id) VALUES (?, ?, ?, ?)
			ON CONFLICT (profile_id, part_id, conversion_id) DO UPDATE SET item_id = excluded.item_id
			RETURNING id`, profile.String(), item.String(), part.String(), conversion).Scan(&added).Error
	})
	if err != nil {
		return Download{}, err
	}
	return s.Download(ctx, profile, uuid.UUID(added.ID))
}

// downloadRow is a download read with its part and conversion.
type downloadRow struct {
	ID             model.UUID
	ItemID         model.UUID
	PartID         model.UUID
	CreatedAt      time.Time
	ConversionID   *model.UUID
	MaxBitrateKbps int
	MaxWidth       int
	State          domain.DownloadState
	Progress       float64
	SizeBytes      int64
	Error          string
}

// downloads reads a profile's downloads, the newest first, or the one of them with an id.
func (s *Store) downloads(ctx context.Context, profile uuid.UUID, id *uuid.UUID) ([]Download, error) {
	sql := `
		SELECT d.id, d.item_id, d.part_id, d.created_at, d.conversion_id,
			coalesce(c.max_bitrate_kbps, 0) AS max_bitrate_kbps, coalesce(c.max_width, 0) AS max_width,
			coalesce(c.state, 'ready') AS state, coalesce(c.progress, 1) AS progress,
			coalesce(c.size_bytes, CASE WHEN c.id IS NULL THEN p.size_bytes END, 0) AS size_bytes,
			coalesce(c.error, '') AS error
		FROM downloads d JOIN parts p ON p.id = d.part_id LEFT JOIN conversions c ON c.id = d.conversion_id
		WHERE d.profile_id = ?`
	args := []any{profile.String()}
	if id != nil {
		sql += ` AND d.id = ?`
		args = append(args, id.String())
	}
	var rows []downloadRow
	if err := s.q.Download.WithContext(ctx).UnderlyingDB().Raw(sql+` ORDER BY d.created_at DESC, d.id`, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Download, len(rows))
	for n, r := range rows {
		out[n] = Download{
			ID: uuid.UUID(r.ID), Item: uuid.UUID(r.ItemID), Part: uuid.UUID(r.PartID), State: r.State,
			Progress: r.Progress, SizeBytes: r.SizeBytes, Error: r.Error, Created: r.CreatedAt,
		}
		if r.ConversionID != nil {
			out[n].Conversion = uuid.UUID(*r.ConversionID)
			out[n].Quality = &domain.Quality{MaxBitrateKbps: r.MaxBitrateKbps, MaxWidth: r.MaxWidth}
		}
	}
	return out, nil
}

// Downloads answers a profile's downloads, the newest first.
func (s *Store) Downloads(ctx context.Context, profile uuid.UUID) ([]Download, error) {
	return s.downloads(ctx, profile, nil)
}

// Download answers one of a profile's downloads. ErrNotFound for none of that id of the profile's.
func (s *Store) Download(ctx context.Context, profile, id uuid.UUID) (Download, error) {
	d, err := s.downloads(ctx, profile, &id)
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
	return s.q.Transaction(func(tx *query.Query) error {
		d := tx.Download
		res, err := d.WithContext(ctx).Where(d.ID.Eq(model.UUID(id)), d.ProfileID.Eq(model.UUID(profile))).Delete()
		if err == nil && res.RowsAffected == 0 {
			err = ErrNotFound
		}
		if err != nil {
			return err
		}
		return dropUnwanted(ctx, tx)
	})
}

// ExpireDownloads forgets every download that has been ready, or failed, since before a time, and
// the conversions no download needs any more. It answers how many downloads.
func (s *Store) ExpireDownloads(ctx context.Context, before time.Time) (int64, error) {
	var n int64
	err := s.q.Transaction(func(tx *query.Query) error {
		res := tx.Download.WithContext(ctx).UnderlyingDB().Exec(`
			DELETE FROM downloads d WHERE d.created_at < ? AND NOT EXISTS (
				SELECT 1 FROM conversions c WHERE c.id = d.conversion_id
					AND (c.state IN ('queued', 'converting') OR c.finished_at >= ?))`, before, before)
		if res.Error != nil {
			return res.Error
		}
		n = res.RowsAffected
		return dropUnwanted(ctx, tx)
	})
	return n, err
}

// dropUnwanted removes the conversions no download needs, and their jobs not yet run.
func dropUnwanted(ctx context.Context, tx *query.Query) error {
	db := tx.Conversion.WithContext(ctx).UnderlyingDB()
	if err := db.Exec(`DELETE FROM conversions c WHERE NOT EXISTS (SELECT 1 FROM downloads d WHERE d.conversion_id = c.id)`).Error; err != nil {
		return err
	}
	return db.Exec(`
		DELETE FROM jobs j WHERE j.kind = 'convert' AND j.state IN ('queued', 'dead')
			AND NOT EXISTS (SELECT 1 FROM conversions c WHERE c.id = j.subject)`).Error
}

// Conversion is what converting a part needs: the part, the quality, and its copy as deciding how
// it plays needs it.
type Conversion struct {
	Part        uuid.UUID
	Quality     domain.Quality
	Container   string
	BitrateKbps int
	Duration    time.Duration
	Streams     []media.Stream
}

// StartConversion records that a node is converting a part, and answers what it needs to.
// ErrNotFound for a conversion that is no longer wanted, or made or failed already.
func (s *Store) StartConversion(ctx context.Context, id, node uuid.UUID) (Conversion, error) {
	c, p, v, st := s.q.Conversion, s.q.Part, s.q.Version, s.q.Stream
	res, err := c.WithContext(ctx).
		Where(c.ID.Eq(model.UUID(id)), c.State.In(string(domain.DownloadQueued), string(domain.DownloadConverting))).
		UpdateSimple(c.State.Value(string(domain.DownloadConverting)), c.NodeID.Value(model.UUID(node)), c.Progress.Value(0))
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return Conversion{}, err
	}
	var row struct {
		PartID         model.UUID
		MaxBitrateKbps int
		MaxWidth       int
		Container      string
		BitrateKbps    int
		DurationMS     int64
	}
	if err := c.WithContext(ctx).Select(c.PartID, c.MaxBitrateKbps, c.MaxWidth, v.Container, v.BitrateKbps, p.DurationMS).
		Join(p, p.ID.EqCol(c.PartID)).Join(v, v.ID.EqCol(p.VersionID)).Where(c.ID.Eq(model.UUID(id))).Scan(&row); err != nil {
		return Conversion{}, err
	}
	streams, err := st.WithContext(ctx).Where(st.PartID.Eq(row.PartID)).Order(st.Idx).Find()
	if err != nil {
		return Conversion{}, err
	}
	out := Conversion{
		Part: uuid.UUID(row.PartID), Quality: domain.Quality{MaxBitrateKbps: row.MaxBitrateKbps, MaxWidth: row.MaxWidth},
		Container: row.Container, BitrateKbps: row.BitrateKbps, Duration: time.Duration(row.DurationMS) * time.Millisecond,
	}
	for _, t := range streams {
		out.Streams = append(out.Streams, mediaStream(t))
	}
	return out, nil
}

// converting updates a conversion a node is still making. ErrNotFound where it is no longer
// wanted, or another node has taken it.
func (s *Store) converting(ctx context.Context, id, node uuid.UUID, columns ...field.AssignExpr) error {
	c := s.q.Conversion
	res, err := c.WithContext(ctx).Where(c.ID.Eq(model.UUID(id)), c.NodeID.Eq(model.UUID(node)),
		c.State.Eq(string(domain.DownloadConverting))).UpdateSimple(columns...)
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

// ConversionProgress records how far a conversion has got, from 0 to 1.
func (s *Store) ConversionProgress(ctx context.Context, id, node uuid.UUID, progress float64) error {
	return s.converting(ctx, id, node, s.q.Conversion.Progress.Value(progress))
}

// FinishConversion records a conversion made, and its file's size.
func (s *Store) FinishConversion(ctx context.Context, id, node uuid.UUID, size int64) error {
	c := s.q.Conversion
	return s.converting(ctx, id, node, c.State.Value(string(domain.DownloadReady)), c.Progress.Value(1),
		c.SizeBytes.Value(size), c.FinishedAt.Value(time.Now()))
}

// FailConversion records why a conversion could not be made.
func (s *Store) FailConversion(ctx context.Context, id, node uuid.UUID, reason string) error {
	c := s.q.Conversion
	return s.converting(ctx, id, node, c.State.Value(string(domain.DownloadFailed)), c.Error.Value(reason),
		c.FinishedAt.Value(time.Now()))
}

// RequeueConversion puts a conversion a node had to stop back in the queue, to be made from the
// beginning.
func (s *Store) RequeueConversion(ctx context.Context, id, node uuid.UUID) error {
	c := s.q.Conversion
	return s.converting(ctx, id, node, c.State.Value(string(domain.DownloadQueued)), c.Progress.Value(0), c.NodeID.Null())
}

// ConversionsOn answers the conversions whose files a node is making or holds.
func (s *Store) ConversionsOn(ctx context.Context, node uuid.UUID) ([]uuid.UUID, error) {
	c := s.q.Conversion
	var ids []model.UUID
	err := c.WithContext(ctx).Where(c.NodeID.Eq(model.UUID(node)),
		c.State.In(string(domain.DownloadConverting), string(domain.DownloadReady))).Pluck(c.ID, &ids)
	out := make([]uuid.UUID, len(ids))
	for n, id := range ids {
		out[n] = uuid.UUID(id)
	}
	return out, err
}

// ConvertedFile answers a download's conversion and the node holding its file. ErrNotFound for no
// such download, or one whose conversion is not ready.
func (s *Store) ConvertedFile(ctx context.Context, download uuid.UUID) (conversion, node uuid.UUID, err error) {
	d, c := s.q.Download, s.q.Conversion
	var row struct {
		ID     model.UUID
		NodeID *model.UUID
	}
	err = d.WithContext(ctx).Select(c.ID, c.NodeID).Join(c, c.ID.EqCol(d.ConversionID)).
		Where(d.ID.Eq(model.UUID(download)), c.State.Eq(string(domain.DownloadReady))).Limit(1).Scan(&row)
	if err == nil && row.NodeID == nil {
		err = ErrNotFound
	}
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, err
	}
	return uuid.UUID(row.ID), uuid.UUID(*row.NodeID), nil
}
