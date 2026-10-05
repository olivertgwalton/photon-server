package domain

import "time"

// Date is a calendar day, written as 2006-01-02.
type Date time.Time

func (d Date) IsZero() bool { return time.Time(d).IsZero() }

func (d Date) MarshalText() ([]byte, error) {
	return []byte(time.Time(d).Format(time.DateOnly)), nil
}
