package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A line logged while a request is served says who was signed in, so an operator can tie it to
// them; one logged outside any request says no one.
func TestALogLineSaysWhoWasSignedIn(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(LogHandler{slog.NewJSONHandler(&out, nil)}).With(slog.String("node", "den"))
	s := domain.Session{ID: uuid.NewV7(), Profile: domain.Profile{ID: uuid.NewV7()}}
	logger.ErrorContext(WithSession(context.Background(), s), "request failed")
	logger.ErrorContext(context.Background(), "scan failed")

	var lines []map[string]any
	for dec := json.NewDecoder(&out); dec.More(); {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatal(err)
		}
		delete(line, "time")
		lines = append(lines, line)
	}
	want := []map[string]any{
		{"level": "ERROR", "msg": "request failed", "node": "den", "profile": s.Profile.ID.String(), "session": s.ID.String()},
		{"level": "ERROR", "msg": "scan failed", "node": "den"},
	}
	if diff := cmp.Diff(want, lines); diff != "" {
		t.Error(diff)
	}
}
