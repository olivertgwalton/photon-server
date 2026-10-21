package auth

import (
	"testing"
	"time"
)

// RFC 6238's SHA-1 test vectors, as six digits: an authenticator app makes the same codes.
func TestCodesAreAnAuthenticatorAppsCodes(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		step, ok := codeStep(secret, want[:3]+" "+want[3:], time.Unix(unix, 0))
		if !ok || step != unix/30 {
			t.Errorf("at %d: code %s taken for step %d, %v; want %d", unix, want, step, ok, unix/30)
		}
	}
	if _, ok := codeStep(secret, "287082", time.Unix(59+90, 0)); ok {
		t.Error("a code three steps old was taken")
	}
}
