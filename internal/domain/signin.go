package domain

// SignInMethod is how a device proves who it signs in as: a profile's password, or a pairing that a
// device already signed in approved.
type SignInMethod string

const (
	SignInPassword SignInMethod = "password"
	SignInPairing  SignInMethod = "pairing"
)

func SignInMethods() []SignInMethod {
	return []SignInMethod{SignInPassword, SignInPairing}
}
