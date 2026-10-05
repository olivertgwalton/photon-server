package auth

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h := newHasher(1)
	encoded, err := h.Hash(t.Context(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash %q is not argon2id with today's parameters", encoded)
	}
	if again, _ := h.Hash(t.Context(), "correct horse"); again == encoded {
		t.Error("two hashes of one password are equal: the salt is not random")
	}
	for _, tt := range []struct {
		password string
		match    bool
	}{{"correct horse", true}, {"correct horse ", false}, {"", false}} {
		match, stale, err := h.Verify(t.Context(), encoded, tt.password)
		if err != nil || match != tt.match || stale {
			t.Errorf("Verify(%q) = %v, stale %v, err %v; want %v", tt.password, match, stale, err, tt.match)
		}
	}
	for _, bad := range []string{"", "plaintext", "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$a2V5", "$argon2id$v=19$m=x$a$b"} {
		if _, _, err := h.Verify(t.Context(), bad, "correct horse"); err == nil {
			t.Errorf("Verify accepted the malformed hash %q", bad)
		}
	}
}
