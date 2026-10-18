package domain

// ServerSettings are what the server is called, the language and country its metadata is asked
// in where a library or title says none, and whether it looks for a newer release of itself, as an
// admin sets them. No name is each node's host's; no country is none.
type ServerSettings struct {
	Name         string
	Locale       Locale
	ReleaseCheck ReleaseCheck
}
