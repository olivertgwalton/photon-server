package domain

// Keep is how a signed-in device holds its session: an app holds the token, a browser a cookie the
// server sets, which its pages' script never sees.
type Keep string

const (
	KeepToken  Keep = "token"
	KeepCookie Keep = "cookie"
)

func Keeps() []Keep {
	return []Keep{KeepToken, KeepCookie}
}
