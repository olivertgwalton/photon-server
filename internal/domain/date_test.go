package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// A date is read as 2006-01-02, an empty one as no date at all, as a client sending every field
// of a form writes one left empty; anything else is refused.
func TestADateIsReadAsADayOrAsNone(t *testing.T) {
	var day Date
	if err := json.Unmarshal([]byte(`"1995-12-15"`), &day); err != nil || time.Time(day).Year() != 1995 {
		t.Errorf("1995-12-15 read as %v, %v", day, err)
	}
	none := day
	if err := json.Unmarshal([]byte(`""`), &none); err != nil || !none.IsZero() {
		t.Errorf(`"" read as %v, %v, want no date`, none, err)
	}
	if err := json.Unmarshal([]byte(`"15/12/1995"`), &day); err == nil {
		t.Error("15/12/1995 read as a date")
	}
}
