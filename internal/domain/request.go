package domain

import "uuid"

// RequestState is where a profile's request for a title stands: waiting on an admin, or the
// manager that keeps the profile, or approved or declined by one.
type RequestState string

const (
	RequestPending  RequestState = "pending"
	RequestApproved RequestState = "approved"
	RequestDeclined RequestState = "declined"
)

func RequestStates() []RequestState {
	return []RequestState{RequestPending, RequestApproved, RequestDeclined}
}

// Wanted is a title a library lacks, as a provider that fetches titles is asked for it: by every id
// its library's search knows it by, and of a show the seasons asked for, every one where none are.
// ID is the request's, the same each time it is sent.
type Wanted struct {
	ID      uuid.UUID
	Kind    ItemKind
	IDs     map[Provider]string
	Title   string
	Year    int
	Seasons []int
}
