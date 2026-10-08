package domain

// ServerSettings are what the server is called, and the language and country its metadata is
// asked in where a library or title says none, as an admin sets them. No name is each node's
// host's; no country is none.
type ServerSettings struct {
	Name   string
	Locale Locale
}
