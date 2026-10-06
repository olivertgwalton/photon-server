package playback

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Fits reports whether a copy is downloaded as it is at a quality: its bitrate no more than the
// most, one nobody knows taken as a transcode takes it, and its picture no wider. Its codecs do
// not matter.
func Fits(c Copy, q domain.Quality) bool {
	video, _ := pick(c.Streams, nil)
	return cmp.Or(c.BitrateKbps, sourceKbps) <= q.MaxBitrateKbps &&
		(q.MaxWidth == 0 || video == nil || video.Width <= q.MaxWidth)
}

// Conversion decides how a copy is converted to a quality for downloading, as it would be
// transcoded for a client playing plays and AAC within it: video encoded to HEVC or H.264 within
// the bitrate and width, HDR kept or tone mapped to SDR as the client shows it, and audio AAC.
// ErrNoCompatibleStream for a copy with no video, or none plays can be made of.
func Conversion(c Copy, q domain.Quality, plays []VideoSupport, hevc domain.HEVCEncoding) (Decision, error) {
	c.BitrateKbps = cmp.Or(c.BitrateKbps, sourceKbps)
	video := make([]VideoSupport, len(plays))
	for i, v := range plays {
		video[i] = VideoSupport{Codec: v.Codec, MaxWidth: q.MaxWidth, Ranges: v.Ranges}
	}
	return Decide(Profile{Video: video, Audio: []AudioSupport{{Codec: "aac"}}, MaxBitrateKbps: q.MaxBitrateKbps}, c, nil, nil, hevc)
}

// Converted is what a device plays of a conversion decided already: its codec, in its range.
func Converted(q domain.Quality) []VideoSupport {
	return []VideoSupport{{Codec: string(q.Codec), Ranges: []domain.Range{q.Range}}}
}

// MaxConversions is how many conversions a node makes at once: Plex's downloads transcode one at
// a time, so a playback is not starved of the processor. Each also holds one of the node's
// transcode slots, which a playback takes from it.
const MaxConversions = 1

// progressEvery is how often a conversion's progress is recorded.
const progressEvery = 5 * time.Second

type conversionStore interface {
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	StartConversion(ctx context.Context, id, node uuid.UUID) (store.Conversion, error)
	ConversionProgress(ctx context.Context, id, node uuid.UUID, progress float64) error
	FinishConversion(ctx context.Context, id, node uuid.UUID, size int64) error
	FailConversion(ctx context.Context, id, node uuid.UUID, reason string) error
	RequeueConversion(ctx context.Context, id, node uuid.UUID) error
	ConversionsOn(ctx context.Context, node uuid.UUID) ([]uuid.UUID, error)
	RemoveDownload(ctx context.Context, profile, id uuid.UUID) error
	ConvertedFile(ctx context.Context, download uuid.UUID) (conversion, node uuid.UUID, err error)
}

// transcodeSlots are the node's slots for encoding video, shared with its playbacks.
type transcodeSlots interface {
	HoldConversion(ctx context.Context) (held context.Context, release func(), ok bool)
}

type nodeAddresses interface {
	NodeAddress(ctx context.Context, id uuid.UUID) (string, bool, error)
}

// Conversions makes downloads' conversions on this node, each a file of its own in dir while a
// download needs it.
type Conversions struct {
	store  conversionStore
	nodes  nodeAddresses
	slots  transcodeSlots
	ffmpeg string
	hw     hls.Hardware
	dir    string
	node   uuid.UUID
}

func NewConversions(st conversionStore, nodes nodeAddresses, slots transcodeSlots, ffmpeg string, hw hls.Hardware, dir string, node uuid.UUID) (*Conversions, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Conversions{store: st, nodes: nodes, slots: slots, ffmpeg: ffmpeg, hw: hw, dir: dir, node: node}, nil
}

func (c *Conversions) path(conversion uuid.UUID) string {
	return filepath.Join(c.dir, conversion.String()+".mp4")
}

// Convert is the job that makes a conversion. It is written to a temporary name and renamed once
// whole; one no longer wanted is stopped and its file removed. One ffmpeg cannot make is recorded
// as failed with its reason, not tried again: a client asking again tries again. It waits, queued,
// for a free transcode slot, and one stopped for a playback starts again from the beginning.
func (c *Conversions) Convert(ctx context.Context, id uuid.UUID) error {
	held, release, ok := c.slots.HoldConversion(ctx)
	if !ok {
		return jobs.ErrNotNow
	}
	defer release()
	job, err := c.store.StartConversion(ctx, id, c.node)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	d, err := Conversion(Copy{Container: job.Container, BitrateKbps: job.BitrateKbps, Streams: job.Streams}, job.Quality, Converted(job.Quality), c.hw.HEVC)
	if err != nil {
		return c.fail(ctx, id, err)
	}
	src, err := openFile(ctx, c.store.PartFile, job.Part)
	if err != nil {
		return c.fail(ctx, id, err)
	}
	defer src.Close()
	dst := c.path(id)
	temp := dst + ".part"
	var reported time.Time
	err = c.hw.Convert(held, c.ffmpeg, src, *d.Video, d.Audio, job.Duration, temp, func(p float64) error {
		if time.Since(reported) < progressEvery {
			return nil
		}
		reported = time.Now()
		return c.store.ConversionProgress(ctx, id, c.node, p)
	})
	if err == nil {
		err = os.Rename(temp, dst)
	}
	if err != nil {
		_ = os.Remove(temp)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, store.ErrNotFound):
			return nil
		case errors.Is(err, hls.ErrPreempted):
			err := c.store.RequeueConversion(ctx, id, c.node)
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return cmp.Or(err, jobs.ErrNotNow)
		}
		return c.fail(ctx, id, err)
	}
	info, err := os.Stat(dst)
	if err == nil {
		err = c.store.FinishConversion(ctx, id, c.node, info.Size())
	}
	if errors.Is(err, store.ErrNotFound) {
		return os.Remove(dst)
	}
	return err
}

func (c *Conversions) fail(ctx context.Context, id uuid.UUID, reason error) error {
	err := c.store.FailConversion(ctx, id, c.node, reason.Error())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

// Prune removes the files of this node's conversions no download needs any more. The folder is
// read before the conversions are, so a conversion just started is already recorded.
func (c *Conversions) Prune(ctx context.Context) error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	held, err := c.store.ConversionsOn(ctx, c.node)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		id, err := uuid.Parse(strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".part"), ".mp4"))
		if err != nil || !slices.Contains(held, id) {
			errs = append(errs, os.Remove(filepath.Join(c.dir, e.Name())))
		}
	}
	return errors.Join(errs...)
}

// Remove forgets one of a profile's downloads, and removes its conversion's file where no other
// download needs it and this node holds it; another node's prune removes its own.
func (c *Conversions) Remove(ctx context.Context, profile, id uuid.UUID) error {
	if err := c.store.RemoveDownload(ctx, profile, id); err != nil {
		return err
	}
	return c.Prune(ctx)
}

// File opens a ready download's conversion where this node holds it, or answers the address of
// the node that does. store.ErrNotFound for no such download, one not ready, or one whose node
// has gone.
func (c *Conversions) File(ctx context.Context, download uuid.UUID) (*os.File, string, error) {
	conversion, node, err := c.store.ConvertedFile(ctx, download)
	if err != nil {
		return nil, "", err
	}
	if node != c.node {
		address, ok, err := c.nodes.NodeAddress(ctx, node)
		if err == nil && !ok {
			err = store.ErrNotFound
		}
		return nil, address, err
	}
	f, err := os.Open(c.path(conversion))
	if errors.Is(err, fs.ErrNotExist) {
		err = store.ErrNotFound
	}
	return f, "", err
}
