package model

import (
	"database/sql/driver"
	"fmt"
	"uuid"
)

// All is every table's model, for the generator and the drift test.
func All() []any {
	return []any{
		Server{}, Library{}, Folder{}, Item{}, ExternalID{}, Version{}, Part{}, PartFile{}, SubtitleFile{}, ItemField{}, LibrarySource{}, LibraryRemoteExtra{}, RemoteVideo{}, Artwork{}, Theme{}, WatchState{}, Favourite{}, Stream{}, Chapter{}, Marker{}, TaskState{}, Job{}, Profile{}, DeviceSession{}, Rating{}, Provider{}, Plugin{}, Collection{}, CollectionMember{}, Playlist{}, PlaylistEntry{}, Person{}, PersonExternalID{}, Credit{}, ProfileLibrary{}, ProfilePreference{}, HomeSection{}, Play{}, Conversion{}, Download{}, Activity{}, Webhook{}, WebhookEvent{}, WebhookDelivery{},
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
