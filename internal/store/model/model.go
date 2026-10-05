package model

import (
	"database/sql/driver"
	"fmt"
	"uuid"
)

// All is every table's model, for the generator and the drift test.
func All() []any {
	return []any{
		Server{}, Library{}, Folder{}, Item{}, ExternalID{}, Version{}, Part{}, PartFile{}, SubtitleFile{}, ItemField{}, LibrarySource{}, LibraryRemoteExtra{}, RemoteVideo{}, Artwork{}, Stream{}, Chapter{}, TaskState{}, Job{}, Profile{}, DeviceSession{},
	}
}

// UUID is the standard library's uuid.UUID as a column; pgx hands a uuid to database/sql as text.
type UUID uuid.UUID

func (u *UUID) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("uuid column: cannot scan %T", src)
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	*u = UUID(id)
	return nil
}

func (u UUID) Value() (driver.Value, error) {
	return uuid.UUID(u).String(), nil
}
