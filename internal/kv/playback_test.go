//go:build integration

package kv

import (
	"os"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAPlaybackIsKeptForItsLife(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"))
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
	left, err := k.client.Do(ctx, k.client.B().Pttl().Key(playbackKey(want.ID)).Build()).AsInt64()
	if err != nil || left <= 0 || left > time.Minute.Milliseconds() {
		t.Errorf("life left = %d ms, %v; want up to a minute", left, err)
	}
	if err := k.EndPlayback(ctx, want.ID); err != nil {
		t.Fatal(err)
	}
	if all, err := k.Playbacks(ctx); err != nil || slices.ContainsFunc(all, same) {
		t.Errorf("after ending: Playbacks = %d, %v; want it gone", len(all), err)
	}
	if _, ok, err := k.Playback(ctx, want.ID); ok || err != nil {
		t.Errorf("after ending: still there %v, %v", ok, err)
	}
}

func TestANodeIsReachedWhileItSaysWhere(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"))
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
	k, err := Open(os.Getenv("TEST_VALKEY_URL"))
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	if v, err := k.Version(t.Context()); v == "" || err != nil {
		t.Errorf("Version = %q, %v", v, err)
	}
}
