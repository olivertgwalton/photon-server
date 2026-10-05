package domain

// DownloadState is how far a download's file is from being fetched.
type DownloadState string

const (
	// DownloadQueued is a conversion waiting for a node to take it.
	DownloadQueued DownloadState = "queued"
	// DownloadConverting is a conversion under way.
	DownloadConverting DownloadState = "converting"
	// DownloadReady is a file to fetch: the part as it is, or its conversion.
	DownloadReady DownloadState = "ready"
	// DownloadFailed is a conversion that could not be made, and why is kept.
	DownloadFailed DownloadState = "failed"
)

func DownloadStates() []DownloadState {
	return []DownloadState{DownloadQueued, DownloadConverting, DownloadReady, DownloadFailed}
}

// Quality is the most a download may be: a video bitrate, and a picture width where it is not
// zero.
type Quality struct {
	MaxBitrateKbps int
	MaxWidth       int
}
