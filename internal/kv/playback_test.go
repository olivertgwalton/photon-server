//go:build integration

package kv

import (
	"context"
	"os"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAPlaybackIsKeptForItsLife(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	started := time.Unix(1_800_000_000, 0)
	want := domain.Playback{
		ID: uuid.NewV7(), Profile: uuid.NewV7(), Item: uuid.NewV7(), Version: uuid.NewV7(),
		Method: domain.PlayRemux, State: domain.StatePaused, Position: 90 * time.Second,
		Started: started, Updated: started.Add(time.Minute), Node: uuid.NewV7(),
		Card: domain.PlaybackCard{
			Profile: domain.PlaybackProfile{ID: uuid.NewV7(), Name: "Oliver"},
			Title:   domain.PlaybackTitle{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Heat"},
			Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			Video:   &domain.PlaybackVideo{Codec: "hevc", Encode: &domain.PlaybackEncode{Codec: "h264", ToneMapped: true}},
		},
	}
	if err := k.SavePlayback(ctx, want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok, err := k.Playback(ctx, want.ID)
	if err != nil || !ok || !cmp.Equal(got, want) {
		t.Fatalf("Playback = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	same := func(p domain.Playback) bool { return p.ID == want.ID }
	if all, err := k.Playbacks(ctx); err != nil || !slices.ContainsFunc(all, same) {
		t.Errorf("Playbacks = %d, %v; want it among them", len(all), err)
	}
	// Valkey lapses it on its own clock; what is asked of it is the life it was given.
	left, err := k.client.Do(ctx, k.client.B().Hpttl().Key(k.key(playbacks)).Fields().Numfields(1).Field(want.ID.String()).Build()).AsIntSlice()
	if err != nil || len(left) != 1 || left[0] <= 0 || left[0] > time.Minute.Milliseconds() {
		t.Errorf("life left = %d ms, %v; want up to a minute", left, err)
	}
	if ended, err := k.EndPlayback(ctx, want.ID); !ended || err != nil {
		t.Fatalf("EndPlayback = %v, %v; want it ended", ended, err)
	}
	if ended, err := k.EndPlayback(ctx, want.ID); ended || err != nil {
		t.Errorf("ending it again = %v, %v; want it already gone", ended, err)
	}
	if all, err := k.Playbacks(ctx); err != nil || slices.ContainsFunc(all, same) {
		t.Errorf("after ending: Playbacks = %d, %v; want it gone", len(all), err)
	}
	if _, ok, err := k.Playback(ctx, want.ID); ok || err != nil {
		t.Errorf("after ending: still there %v, %v", ok, err)
	}
}

func TestANodeIsReachedWhileItSaysWhere(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	node := uuid.NewV7()
	if _, ok, err := k.NodeAddress(ctx, node); ok || err != nil {
		t.Fatalf("before it said: %v, %v", ok, err)
	}
	if err := k.SetNode(ctx, node, "http://10.0.0.5:8640", time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.NodeAddress(ctx, node); !ok || err != nil || got != "http://10.0.0.5:8640" {
		t.Errorf("NodeAddress = %q, %v, %v", got, ok, err)
	}
	nodes, err := k.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(nodes, func(n Node) bool { return n.ID == node })
	if i < 0 || nodes[i].Address != "http://10.0.0.5:8640" || time.Since(nodes[i].Seen) > time.Minute {
		t.Errorf("Nodes = %+v, want %v among them, seen just now", nodes, node)
	}
}

func TestValkeySaysItsVersion(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	if v, err := k.Version(t.Context()); v == "" || err != nil {
		t.Errorf("Version = %q, %v", v, err)
	}
}

// counting is a client that counts the round trips made through it and the commands sent.
type counting struct {
	valkey.Client
	mu    sync.Mutex
	trips int
	sent  []string
}

func (c *counting) note(cmds ...valkey.Completed) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trips++
	for _, cmd := range cmds {
		c.sent = append(c.sent, cmd.Commands()[0])
	}
}

func (c *counting) Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	c.note(cmd)
	return c.Client.Do(ctx, cmd)
}

func (c *counting) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	c.note(cmds...)
	return c.Client.DoMulti(ctx, cmds...)
}

// Listing what is going on reads the few keys it lists, together, however many other keys a
// shared Valkey holds: it never walks the keyspace.
func TestListingReadsNoKeyItDoesNotList(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	var ids []uuid.UUID
	for range 5 {
		id := uuid.NewV7()
		ids = append(ids, id)
		if err := k.SavePlayback(ctx, domain.Playback{ID: id, Card: domain.PlaybackCard{}}, time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := k.SetNode(ctx, id, "http://10.0.0.5:8640", time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := k.SaveScan(ctx, domain.ScanProgress{Library: id, Phase: domain.ScanReading}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = k.EndPlayback(context.WithoutCancel(ctx), id)
			_ = k.EndScan(context.WithoutCancel(ctx), id)
		}
	})
	c := &counting{Client: k.client}
	k.client = c
	plays, err := k.Playbacks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := k.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scans, err := k.Scans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !slices.ContainsFunc(plays, func(p domain.Playback) bool { return p.ID == id }) ||
			!slices.ContainsFunc(nodes, func(n Node) bool { return n.ID == id }) ||
			!slices.ContainsFunc(scans, func(s domain.ScanProgress) bool { return s.Library == id }) {
			t.Fatalf("%v is not listed among %d playbacks, %d nodes and %d scans", id, len(plays), len(nodes), len(scans))
		}
	}
	if slices.Contains(c.sent, "SCAN") || c.trips > 3 {
		t.Errorf("listing three kinds took %d round trips, sending %v; want one each and no SCAN", c.trips, slices.Compact(slices.Sorted(slices.Values(c.sent))))
	}
}

// A playback whose player went quiet with no node left to sweep it lapses from the list with its
// record: nothing lists a playback that is gone.
func TestALapsedPlaybackIsListedNowhere(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	lapsing, staying := domain.Playback{ID: uuid.NewV7()}, domain.Playback{ID: uuid.NewV7()}
	if err := k.SavePlayback(ctx, lapsing, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := k.SavePlayback(ctx, staying, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Valkey lapses it on its own clock.
	for lapsed := false; !lapsed; {
		select {
		case <-ctx.Done():
			t.Fatal("it never lapsed")
		case <-time.After(50 * time.Millisecond):
		}
		_, there, err := k.Playback(ctx, lapsing.ID)
		lapsed = !there && err == nil
	}
	all, err := k.Playbacks(ctx)
	if err != nil || len(all) != 1 || all[0].ID != staying.ID {
		t.Errorf("Playbacks = %+v, %v; want only the one still going", all, err)
	}
	if ended, err := k.EndPlayback(ctx, lapsing.ID); ended || err != nil {
		t.Errorf("ending the lapsed one = %v, %v; want it already gone", ended, err)
	}
}

// Servers sharing a Valkey keep apart: one's playbacks and events are not another's.
func TestServersSharingAValkeyKeepApart(t *testing.T) {
	mine, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer mine.Close()
	theirs, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	defer theirs.Close()
	ctx := t.Context()
	if err := theirs.SavePlayback(ctx, domain.Playback{ID: uuid.NewV7()}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if all, err := mine.Playbacks(ctx); err != nil || len(all) != 0 {
		t.Errorf("another server's playbacks are listed here: %d, %v", len(all), err)
	}
}
