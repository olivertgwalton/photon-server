package domain

import (
	"fmt"
	"time"
)

// Date is a calendar day, written as 2006-01-02.
type Date time.Time

func (d Date) IsZero() bool { return time.Time(d).IsZero() }

func (d Date) MarshalText() ([]byte, error) {
	return []byte(time.Time(d).Format(time.DateOnly)), nil
}

// UnmarshalText reads "" as no date, as a form with the field left empty sends it.
func (d *Date) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*d = Date{}
		return nil
	}
	t, err := time.Parse(time.DateOnly, string(text))
	if err != nil {
		return fmt.Errorf("%q is not a date, 2006-01-02", text)
	}
	*d = Date(t)
	return nil
}
