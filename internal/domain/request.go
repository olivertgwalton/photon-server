package domain

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
