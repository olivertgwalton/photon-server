//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// downloadable makes a film of one 1080p part, and two profiles to download it.
func downloadable(t *testing.T) (s *Store, film, part uuid.UUID, profiles [2]uuid.UUID) {
	t.Helper()
	s = migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	p := Part{RelPath: "L/L.mkv", Size: 6_000_000_000, ModTime: time.Unix(0, 0), Facts: &media.Facts{
		Container: "matroska,webm", Duration: 2 * time.Hour, BitrateKbps: 8000,
		Streams: []media.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080, Range: domain.RangeSDR}},
	}}
	if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{{Title: "Lawrence", Folder: "L", Copies: []Copy{{ContentKey: []byte("k"), Parts: []Part{p}}}}}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.q.Part.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	for n, name := range []string{"Oliver", "Ada"} {
		pr, err := s.AddProfile(ctx, name, domain.RoleMember, "")
		if err != nil {
			t.Fatal(err)
		}
		profiles[n] = pr.ID
	}
	return s, uuid.UUID(item.ID), uuid.UUID(row.ID), profiles
}

func (s *Store) convertJobs(t *testing.T) int64 {
	t.Helper()
	j := s.q.Job
	n, err := j.WithContext(t.Context()).Where(j.Kind.Eq(string(domain.JobConvert))).Count()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The part's own file is ready at once; a conversion is made once for every profile asking for
// the same quality, and forgotten, with its job, when the last of their downloads is removed.
func TestAConversionIsSharedUntilNoDownloadNeedsIt(t *testing.T) {
	s, film, part, profiles := downloadable(t)
	ctx := t.Context()
	original, err := s.AddDownload(ctx, profiles[0], film, part, nil)
	if err != nil || original.State != domain.DownloadReady || original.Quality != nil || original.SizeBytes != 6_000_000_000 {
		t.Fatalf("the original: %+v, %v; want it ready at the part's size", original, err)
	}
	if n := s.convertJobs(t); n != 0 {
		t.Errorf("%d conversions queued for the original, want none", n)
	}
	q := domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1280}
	mine, err := s.AddDownload(ctx, profiles[0], film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.AddDownload(ctx, profiles[1], film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddDownload(ctx, profiles[0], film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	if mine.Conversion != theirs.Conversion || mine.ID == theirs.ID || again.ID != mine.ID || mine.State != domain.DownloadQueued {
		t.Fatalf("mine %+v, theirs %+v, again %+v; want two queued downloads of one conversion", mine, theirs, again)
	}
	if n := s.convertJobs(t); n != 1 {
		t.Errorf("%d conversions queued, want one", n)
	}
	node := uuid.NewV7()
	c, err := s.StartConversion(ctx, mine.Conversion, node)
	if err != nil || c.Part != part || c.Quality != q || c.Duration != 2*time.Hour || len(c.Streams) != 1 {
		t.Fatalf("StartConversion = %+v, %v", c, err)
	}
	if err := s.FinishConversion(ctx, mine.Conversion, node, 1_800_000_000); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Download(ctx, profiles[1], theirs.ID); err != nil || got.State != domain.DownloadReady || got.SizeBytes != 1_800_000_000 {
		t.Errorf("their download once made: %+v, %v", got, err)
	}
	if conv, on, err := s.ConvertedFile(ctx, theirs.ID); err != nil || conv != mine.Conversion || on != node {
		t.Errorf("ConvertedFile = %v on %v, %v; want the shared conversion on its node", conv, on, err)
	}
	if err := s.RemoveDownload(ctx, profiles[1], mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing another profile's download: %v, want ErrNotFound", err)
	}
	if err := s.RemoveDownload(ctx, profiles[0], mine.ID); err != nil {
		t.Fatal(err)
	}
	if held, err := s.ConversionsOn(ctx, node); err != nil || len(held) != 1 {
		t.Errorf("with one download left: node holds %v, %v; want the conversion", held, err)
	}
	if err := s.RemoveDownload(ctx, profiles[1], theirs.ID); err != nil {
		t.Fatal(err)
	}
	if held, err := s.ConversionsOn(ctx, node); err != nil || len(held) != 0 {
		t.Errorf("with no download left: node holds %v, %v; want nothing", held, err)
	}
	if n := s.convertJobs(t); n != 0 {
		t.Errorf("%d conversion jobs left, want none", n)
	}
}

// A failed conversion keeps its reason until someone asks for it again, which tries again; a
// download expires after its retention unless it is still being made.
func TestFailedConversionsAreTriedAgainAndFinishedOnesExpire(t *testing.T) {
	s, film, part, profiles := downloadable(t)
	ctx := t.Context()
	q := domain.Quality{MaxBitrateKbps: 1000}
	d, err := s.AddDownload(ctx, profiles[0], film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	node := uuid.NewV7()
	if _, err := s.StartConversion(ctx, d.Conversion, node); err != nil {
		t.Fatal(err)
	}
	if err := s.FailConversion(ctx, d.Conversion, node, "ffmpeg: exit status 1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Download(ctx, profiles[0], d.ID); err != nil || got.State != domain.DownloadFailed || got.Error != "ffmpeg: exit status 1" {
		t.Fatalf("failed: %+v, %v", got, err)
	}
	if _, err := s.StartConversion(ctx, d.Conversion, node); !errors.Is(err, ErrNotFound) {
		t.Errorf("starting a failed conversion: %v, want ErrNotFound", err)
	}
	retried, err := s.AddDownload(ctx, profiles[1], film, part, &q)
	if err != nil || retried.Conversion != d.Conversion || retried.State != domain.DownloadQueued || retried.Error != "" {
		t.Fatalf("asked for again: %+v, %v; want the conversion queued afresh", retried, err)
	}
	if _, err := s.AddDownload(ctx, profiles[0], film, part, nil); err != nil {
		t.Fatal(err)
	}
	n, err := s.ExpireDownloads(ctx, time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Errorf("expired %d, %v; want only the original, the conversion still queued", n, err)
	}
	if _, err := s.StartConversion(ctx, d.Conversion, node); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishConversion(ctx, d.Conversion, node, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ExpireDownloads(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Errorf("expired %d, %v before the retention ran out; want none", n, err)
	}
	if n, err := s.ExpireDownloads(ctx, time.Now().Add(time.Hour)); err != nil || n != 2 {
		t.Errorf("expired %d, %v after it; want both downloads of the conversion", n, err)
	}
	if held, err := s.ConversionsOn(ctx, node); err != nil || len(held) != 0 {
		t.Errorf("the node holds %v, %v; want the conversion forgotten", held, err)
	}
}
