//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

func TestASignInFlowIsTakenOnce(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	state := []byte("state hash")
	flow := SignInFlow{Provider: "id", Issuer: "https://id.example.com", Verifier: "v", Nonce: "n", To: "/"}
	if started, err := k.StartSignIn(ctx, state, flow, time.Minute); err != nil || !started {
		t.Fatalf("starting: %v, %v", started, err)
	}
	if started, err := k.StartSignIn(ctx, state, SignInFlow{Provider: "other"}, time.Minute); err != nil || started {
		t.Fatalf("a state in use was taken for another flow: %v, %v", started, err)
	}
	if got, ok, err := k.TakeSignIn(ctx, state); err != nil || !ok || got != flow {
		t.Fatalf("taking: %+v, %v, %v; want %+v", got, ok, err, flow)
	}
	if _, ok, err := k.TakeSignIn(ctx, state); err != nil || ok {
		t.Fatalf("a flow was taken twice: %v, %v", ok, err)
	}
}
