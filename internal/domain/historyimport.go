package domain

import (
	"time"
	"uuid"
)

// ImportSource is the kind of server a profile's watch history is imported from.
type ImportSource string

const (
	ImportPlex     ImportSource = "plex"
	ImportJellyfin ImportSource = "jellyfin"
	ImportEmby     ImportSource = "emby"
)

func ImportSources() []ImportSource {
	return []ImportSource{ImportPlex, ImportJellyfin, ImportEmby}
}

// ImportStatus is how far an import has got.
type ImportStatus string

const (
	ImportQueued  ImportStatus = "queued"
	ImportRunning ImportStatus = "running"
	ImportDone    ImportStatus = "done"
	// ImportFailed is an import the source stopped answering, and why is kept.
	ImportFailed ImportStatus = "failed"
)

func ImportStatuses() []ImportStatus {
	return []ImportStatus{ImportQueued, ImportRunning, ImportDone, ImportFailed}
}

// ImportMiss is why a title the source had watched was not imported.
type ImportMiss string

const (
	// MissNoIDs is a title the source knows by no TMDB, TheTVDB or IMDb id: titles are matched by
	// those alone, never by name or path.
	MissNoIDs ImportMiss = "no_ids"
	// MissNotFound is a title none here has the ids of, or an episode its show here lacks.
	MissNotFound ImportMiss = "not_found"
	// MissUndated is a title the source says was watched but not when, so it cannot be told newer
	// than what is here.
	MissUndated ImportMiss = "undated"
)

func ImportMisses() []ImportMiss {
	return []ImportMiss{MissNoIDs, MissNotFound, MissUndated}
}

// Missed is a title not imported, and why.
type Missed struct {
	Title  string     `json:"title"`
	Reason ImportMiss `json:"reason"`
}

// HistoryImport is one import of a source server's watch history into a profile. Matched titles
// were found here; of those, Imported changed the profile's state and Skipped did not, as what is
// here is newer or the source gave no date. Misses lists some of the titles not imported.
type HistoryImport struct {
	ID         uuid.UUID
	Source     ImportSource
	URL        string
	Profile    uuid.UUID
	Status     ImportStatus
	Error      string
	Matched    int
	Imported   int
	Skipped    int
	Unmatched  int
	Misses     []Missed
	CreatedAt  time.Time
	FinishedAt *time.Time
}
