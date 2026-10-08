package auth

import (
	"net/netip"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/kv"
)

// What a client may attempt before it is made to wait. A name has its own limit beside the
// address's, so guessing one profile's password from many addresses is slowed as well.
var (
	SignInsPerAddress  = kv.Limit{Every: 6 * time.Second, Burst: 10}
	SignInsPerName     = kv.Limit{Every: 6 * time.Minute, Burst: 10}
	PairingsPerAddress = kv.Limit{Every: 6 * time.Second, Burst: 10}
	// ResetsPerAddress holds both asking for resets, each a line in the log, and guessing codes.
	ResetsPerAddress    = kv.Limit{Every: time.Minute, Burst: 5}
	ApprovalsPerProfile = kv.Limit{Every: 12 * time.Second, Burst: 5}
)

// PairingKey and ApprovalKey are a pairing's allowances, shared by the APIs as SignInKeys are.
func PairingKey(addr netip.Addr) string { return "pairing:addr:" + addr.String() }

func ApprovalKey(profile uuid.UUID) string { return "approve:profile:" + profile.String() }

// SignInKeys are the allowances a sign-in spends, the same through whichever API it comes, so a
// second API never doubles the guesses a client may make.
func SignInKeys(addr netip.Addr, name string) (byAddress, byName string) {
	return "signin:addr:" + addr.String(), "signin:name:" + strings.ToLower(name)
}
