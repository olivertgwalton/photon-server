package httpapi

import (
	"context"
	"uuid"
)

// noCopies is a server with no remote library: every title's copies are the scanner's.
type noCopies struct{}

func (noCopies) Ensure(context.Context, uuid.UUID) error { return nil }
