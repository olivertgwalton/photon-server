package domain

import "uuid"

// RequestState is where a profile's request for a title stands: waiting on an admin, or the
// manager that keeps the profile, or approved or declined by one. An approved request whose library
// names where its requests go is sent there, which took it on or refused it.
type RequestState string

const (
	RequestPending  RequestState = "pending"
	RequestApproved RequestState = "approved"
	RequestDeclined RequestState = "declined"
	RequestSent     RequestState = "sent"
	// RequestFailed is a request where it was sent refused, for an admin to approve again or
	// decline.
	RequestFailed RequestState = "failed"
)

func RequestStates() []RequestState {
	return []RequestState{RequestPending, RequestApproved, RequestDeclined, RequestSent, RequestFailed}
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
