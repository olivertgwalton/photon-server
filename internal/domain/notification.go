package domain

import (
	"time"
	"uuid"
)

// Notify is what a profile is told of as a scan adds it.
type Notify string

const (
	// NotifyMarked is new episodes of the shows the profile has marked: on its watchlist, a
	// favourite, or begun.
	NotifyMarked Notify = "marked"
	// NotifyAll is every new film and episode the profile may see.
	NotifyAll  Notify = "all"
	NotifyNone Notify = "none"
)

func Notifies() []Notify {
	return []Notify{NotifyMarked, NotifyAll, NotifyNone}
}

// NotificationChange is what became of some of a profile's notifications.
type NotificationChange string

const (
	// NotificationsAdded is notifications added, or gathering more of a show's episodes.
	NotificationsAdded NotificationChange = "added"
	NotificationsRead  NotificationChange = "read"
)

func NotificationChanges() []NotificationChange {
	return []NotificationChange{NotificationsAdded, NotificationsRead}
}

// NotificationsDetails says which of a profile's notifications changed, so each of its devices
// changes those alone.
type NotificationsDetails struct {
	Change NotificationChange `json:"change"`
	// IDs are those added or gathering more, or those read.
	IDs []int64 `json:"ids,omitzero"`
	// Through is the added_at up to which every one was read.
	Through time.Time `json:"through,omitzero"`
	// Unread is how many are left unread after a read.
	Unread int `json:"unread"`
}

// Notification is a film, or a show's new episodes, gathered while it is unread: an episode added
// to a show whose notification is unread joins it, so a season added at once is one notification.
type Notification struct {
	ID int64
	// Title is the film or the show, Latest the film or the show's newest episode of those told.
	Title  uuid.UUID
	Latest uuid.UUID
	// New is how many of its films and episodes the profile may see.
	New     int
	AddedAt time.Time
	ReadAt  *time.Time
}
