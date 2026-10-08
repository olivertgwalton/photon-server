//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	p := Part{RelPath: "L/L.mkv", Size: 6_000_000_000, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Container: "matroska,webm", Duration: 2 * time.Hour, BitrateKbps: 8000,
		Streams: []domain.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080, Range: domain.RangeSDR}},
	}}
	if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{{Title: "Lawrence", Folder: "L", Copies: []Copy{{ContentKey: []byte("k"), Parts: []Part{p}}}}}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT id FROM items), (SELECT id FROM parts)`).Scan(&item, &part); err != nil {
		t.Fatal(err)
	}
	for n, name := range []string{"Oliver", "Ada"} {
		pr, err := s.AddProfile(ctx, name, domain.RoleUser, "hash", nil)
		if err != nil {
			t.Fatal(err)
		}
		profiles[n] = pr.ID
	}
	return s, item, part, profiles
}

// signIn signs a device in as a profile.
func (s *Store) signIn(t *testing.T, profile uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := s.CreateSession(t.Context(), NewSession{
		Kind: domain.SessionDevice, ProfileID: profile, TokenHash: []byte(uuid.NewV7().String()),
		DeviceName: "TV", Client: "Photon", ExpiresAt: new(time.Now().Add(time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (s *Store) convertJobs(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind = $1`, domain.JobConvert).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The part's own file is ready at once; a conversion is made once for every profile asking for
// the same quality and video, and forgotten, with its job, when the last of their downloads is removed.
func TestAConversionIsSharedUntilNoDownloadNeedsIt(t *testing.T) {
	s, film, part, profiles := downloadable(t)
	ctx := t.Context()
	devices := [2]uuid.UUID{s.signIn(t, profiles[0]), s.signIn(t, profiles[1])}
	original, _, err := s.AddDownload(ctx, profiles[0], devices[0], film, part, nil)
	if err != nil || original.State != domain.DownloadReady || original.Quality != nil || original.SizeBytes != 6_000_000_000 {
		t.Fatalf("the original: %+v, %v; want it ready at the part's size", original, err)
	}
	if n := s.convertJobs(t); n != 0 {
		t.Errorf("%d conversions queued for the original, want none", n)
	}
	q := domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1280, Codec: domain.VideoH264, Range: domain.RangeSDR}
	mine, created, err := s.AddDownload(ctx, profiles[0], devices[0], film, part, &q)
	if err != nil || !created {
		t.Fatalf("mine: created %v, %v; want it created", created, err)
	}
	theirs, _, err := s.AddDownload(ctx, profiles[1], devices[1], film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := s.AddDownload(ctx, profiles[0], devices[0], film, part, &q)
	if err != nil || created {
		t.Fatalf("again: created %v, %v; want mine answered as it stands", created, err)
	}
	if mine.Conversion != theirs.Conversion || mine.ID == theirs.ID || again.ID != mine.ID || mine.State != domain.DownloadQueued {
		t.Fatalf("mine %+v, theirs %+v, again %+v; want two queued downloads of one conversion", mine, theirs, again)
	}
	if n := s.convertJobs(t); n != 1 {
		t.Errorf("%d conversions queued, want one", n)
	}
	hdr := q
	hdr.Codec, hdr.Range = domain.VideoHEVC, domain.RangeHDR10
	other, _, err := s.AddDownload(ctx, profiles[1], devices[1], film, part, &hdr)
	if err != nil || other.Conversion == mine.Conversion || *other.Quality != hdr {
		t.Fatalf("HEVC HDR10 at the same quality: %+v, %v; want a conversion of its own", other, err)
	}
	if err := s.RemoveDownload(ctx, profiles[1], other.ID); err != nil {
		t.Fatal(err)
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
	devices := [2]uuid.UUID{s.signIn(t, profiles[0]), s.signIn(t, profiles[1])}
	q := domain.Quality{MaxBitrateKbps: 1000, Codec: domain.VideoH264, Range: domain.RangeSDR}
	d, _, err := s.AddDownload(ctx, profiles[0], devices[0], film, part, &q)
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
	retried, _, err := s.AddDownload(ctx, profiles[1], devices[1], film, part, &q)
	if err != nil || retried.Conversion != d.Conversion || retried.State != domain.DownloadQueued || retried.Error != "" {
		t.Fatalf("asked for again: %+v, %v; want the conversion queued afresh", retried, err)
	}
	if _, _, err := s.AddDownload(ctx, profiles[0], devices[0], film, part, nil); err != nil {
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

// A download is the device's that asked for it: another device of the profile asks for its own,
// lists only its own unless it asks for the profile's, and signing a device out forgets its
// downloads.
func TestADownloadIsItsDevices(t *testing.T) {
	s, film, part, profiles := downloadable(t)
	ctx := t.Context()
	tv, phone := s.signIn(t, profiles[0]), s.signIn(t, profiles[0])
	q := domain.Quality{MaxBitrateKbps: 2000, Codec: domain.VideoH264, Range: domain.RangeSDR}
	onTV, _, err := s.AddDownload(ctx, profiles[0], tv, film, part, &q)
	if err != nil {
		t.Fatal(err)
	}
	onPhone, _, err := s.AddDownload(ctx, profiles[0], phone, film, part, &q)
	if err != nil || onPhone.ID == onTV.ID || onPhone.Conversion != onTV.Conversion || onPhone.Device != phone {
		t.Fatalf("the phone's: %+v, %v; want a download of its own, of the TV's conversion", onPhone, err)
	}
	if got, err := s.Downloads(ctx, profiles[0], &tv); err != nil || len(got) != 1 || got[0].ID != onTV.ID {
		t.Errorf("the TV's list: %+v, %v; want its download alone", got, err)
	}
	if got, err := s.Downloads(ctx, profiles[0], nil); err != nil || len(got) != 2 {
		t.Errorf("the profile's list: %+v, %v; want both devices'", got, err)
	}
	if err := s.DeleteSession(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Download(ctx, profiles[0], onPhone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the phone's download once it is signed out: %v, want ErrNotFound", err)
	}
	if got, err := s.Downloads(ctx, profiles[0], nil); err != nil || len(got) != 1 || got[0].ID != onTV.ID {
		t.Errorf("the profile's list once the phone is signed out: %+v, %v; want the TV's", got, err)
	}
	laptop := s.signIn(t, profiles[1])
	alone, _, err := s.AddDownload(ctx, profiles[1], laptop, film, part, &domain.Quality{MaxBitrateKbps: 500, Codec: domain.VideoH264, Range: domain.RangeSDR})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, laptop); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartConversion(ctx, alone.Conversion, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("converting for a device signed out: %v, want ErrNotFound", err)
	}
}

func TestADownloadSaysWhichOfItsCopysFilesItIs(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	disc := func(rel string) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}
	}
	film := Film{Title: "Lawrence", Folder: "L", Copies: []Copy{{ContentKey: []byte("k"), Parts: []Part{disc("L/cd1.mkv"), disc("L/cd2.mkv")}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item, second uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT id FROM items), (SELECT id FROM parts WHERE idx = 1)`).Scan(&item, &second); err != nil {
		t.Fatal(err)
	}
	profile, err := s.AddProfile(ctx, "Oliver", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := s.AddDownload(ctx, profile.ID, s.signIn(t, profile.ID), item, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.PartIndex != 1 || d.Parts != 2 {
		t.Errorf("the second disc's download: part %d of %d, want 1 of 2", d.PartIndex, d.Parts)
	}
}
