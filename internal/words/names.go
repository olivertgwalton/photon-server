package words

import (
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Each value's name, by its type, so "watchlist" the mark and "watchlist" the calendar filter are
// two names. A value with no name is said as it is sent.
func (w Words) Role(v domain.Role) string                     { return label(roles, v) }
func (w Words) MarkerKind(v domain.MarkerKind) string         { return label(markerKinds, v) }
func (w Words) ExtraKind(v domain.ExtraKind) string           { return label(extraKinds, v) }
func (w Words) ImportSource(v domain.ImportSource) string     { return label(importSources, v) }
func (w Words) ImportMiss(v domain.ImportMiss) string         { return label(importMisses, v) }
func (w Words) PlayMethod(v domain.PlayMethod) string         { return label(playMethods, v) }
func (w Words) RatingSite(v domain.RatingSite) string         { return label(ratingSites, v) }
func (w Words) Range(v domain.Range) string                   { return label(ranges, v) }
func (w Words) Resolution(v domain.Resolution) string         { return label(resolutions, v) }
func (w Words) ItemKind(v domain.ItemKind) string             { return label(itemKinds, v) }
func (w Words) LibraryKind(v domain.LibraryKind) string       { return label(libraryKinds, v) }
func (w Words) Mark(v domain.Mark) string                     { return label(marks, v) }
func (w Words) Milestone(v domain.Milestone) string           { return label(milestones, v) }
func (w Words) CalendarFilter(v domain.CalendarFilter) string { return label(calendarFilters, v) }
func (w Words) DownloadState(v domain.DownloadState) string   { return label(downloadStates, v) }
func (w Words) StreamKind(v domain.StreamKind) string         { return label(streamKinds, v) }
func (w Words) JobState(v domain.JobState) string             { return label(jobStates, v) }
func (w Words) Acceleration(v domain.Acceleration) string     { return label(accelerations, v) }
func (w Words) JobKind(v domain.JobKind) string               { return label(jobKinds, v) }

func label[T ~string](names map[T]string, v T) string {
	if n, ok := names[v]; ok {
		return n
	}
	return string(v)
}

var roles = map[domain.Role]string{
	domain.RoleAdmin: "Admin", domain.RoleManager: "Manager", domain.RoleUser: "User",
}

var markerKinds = map[domain.MarkerKind]string{
	domain.MarkerIntro: "Intro", domain.MarkerCredits: "Credits", domain.MarkerRecap: "Recap",
	domain.MarkerPreview: "Preview",
}

var extraKinds = map[domain.ExtraKind]string{
	domain.ExtraTrailer: "Trailer", domain.ExtraTeaser: "Teaser",
	domain.ExtraFeaturette: "Featurette", domain.ExtraBehindTheScenes: "Behind the scenes",
	domain.ExtraDeletedScene: "Deleted scene", domain.ExtraInterview: "Interview",
	domain.ExtraScene: "Scene", domain.ExtraShort: "Short", domain.ExtraClip: "Clip",
	domain.ExtraBlooper: "Blooper", domain.ExtraThemeVideo: "Theme video",
	domain.ExtraOther: "Extra",
}

var importSources = map[domain.ImportSource]string{
	domain.ImportPlex: "Plex", domain.ImportJellyfin: "Jellyfin", domain.ImportEmby: "Emby",
}

var importMisses = map[domain.ImportMiss]string{
	domain.MissNoIDs: "No TMDB, TheTVDB or IMDb id", domain.MissNotFound: "Not in a library here",
	domain.MissUndated: "No date watched",
}

var playMethods = map[domain.PlayMethod]string{
	domain.PlayDirect: "Direct play", domain.PlayRemux: "Direct stream",
	domain.PlayTranscode: "Transcode",
}

var ratingSites = map[domain.RatingSite]string{
	domain.SiteIMDb: "IMDb", domain.SiteTMDB: "TMDB", domain.SiteRottenTomatoes: "Rotten Tomatoes",
	domain.SiteRottenTomatoesAudience: "RT Audience",
}

var ranges = map[domain.Range]string{
	domain.RangeSDR: "SDR", domain.RangeHLG: "HLG", domain.RangeHDR10: "HDR10",
	domain.RangeHDR10Plus: "HDR10+", domain.RangeDV: "Dolby Vision",
}

var resolutions = map[domain.Resolution]string{
	domain.ResolutionSD: "SD", domain.ResolutionHD: "720p", domain.ResolutionFHD: "1080p",
	domain.ResolutionUHD: "4K",
}

var itemKinds = map[domain.ItemKind]string{
	domain.ItemMovie: "Films", domain.ItemShow: "Shows", domain.ItemSeason: "Seasons",
	domain.ItemEpisode: "Episodes", domain.ItemExtra: "Extras",
	domain.ItemCollection: "Collections",
}

var libraryKinds = map[domain.LibraryKind]string{
	domain.LibraryMovies: "Films", domain.LibraryShows: "Shows",
}

var marks = map[domain.Mark]string{
	domain.MarkWatched: "Watched", domain.MarkUnwatched: "Unwatched",
	domain.MarkInProgress: "In progress", domain.MarkFavourite: "Favourites",
	domain.MarkWatchlist: "Watchlist",
}

var milestones = map[domain.Milestone]string{
	domain.MilestoneSeriesPremiere: "Series premiere",
	domain.MilestoneSeasonPremiere: "Season premiere", domain.MilestoneSeasonFinale: "Finale",
}

var calendarFilters = map[domain.CalendarFilter]string{
	domain.CalendarMine: "My titles", domain.CalendarWatchlist: "Watchlist",
	domain.CalendarFavourites: "Favourites", domain.CalendarAll: "Everything",
}

var downloadStates = map[domain.DownloadState]string{
	domain.DownloadQueued: "Waiting", domain.DownloadConverting: "Converting",
	domain.DownloadReady: "Ready", domain.DownloadFailed: "Failed",
}

var streamKinds = map[domain.StreamKind]string{
	domain.StreamVideo: "Video", domain.StreamAudio: "Audio", domain.StreamSubtitle: "Subtitle",
}

var jobStates = map[domain.JobState]string{
	domain.JobQueued: "Queued", domain.JobRunning: "Running", domain.JobRerun: "To run again",
	domain.JobDead: "Gave up",
}

var accelerations = map[domain.Acceleration]string{
	domain.AccelSoftware: "Software", domain.AccelVideoToolbox: "VideoToolbox",
	domain.AccelVAAPI: "VA-API", domain.AccelQSV: "Quick Sync", domain.AccelNVENC: "NVENC",
}

var rows = map[domain.HomeRow]string{
	domain.RowContinueWatching: "Continue Watching", domain.RowNextUp: "Next Up",
	domain.RowWatchlist: "Watchlist", domain.RowFavourites: "Favourites",
	domain.RowRecentFilms: "Recently Added Films", domain.RowRecentShows: "Recently Added Shows",
	domain.RowRecentlyReleased: "Recently Released", domain.RowTopRatedUnwatched: "Top Rated",
	domain.RowCollection: "Collections",
}

var jobKinds = map[domain.JobKind]string{
	domain.JobKeyframes: "Read keyframes", domain.JobKeyframeWalk: "Walk files for keyframes",
	domain.JobIdentify: "Identify", domain.JobScanLibrary: "Scan a library",
	domain.JobMarkers: "Find intros and credits", domain.JobPreviews: "Make previews",
	domain.JobConvert: "Convert for download", domain.JobDeliverWebhook: "Send a webhook",
	domain.JobTheme: "Fetch a theme tune", domain.JobProbe: "Read media info",
	domain.JobImportHistory: "Import watch history",
}

// Kept is how the activity log's filter names a kind of event it keeps: "Plays started".
func (w Words) Kept(k domain.EventKind) string {
	return kept[k]
}

var kept = map[domain.EventKind]string{
	domain.EventPlaybackStarted: "Plays started", domain.EventPlaybackStopped: "Plays stopped",
	domain.EventSignedIn: "Sign-ins", domain.EventSignInRefused: "Refused sign-ins",
	domain.EventProfileAdded: "Profiles added", domain.EventProfileRemoved: "Profiles removed",
	domain.EventLibraryAdded: "Libraries added", domain.EventLibraryRemoved: "Libraries removed",
	domain.EventLibraryScanned: "Scans", domain.EventTitlesAdded: "Titles added", domain.EventTaskFailed: "Failed tasks",
	domain.EventBackupMade: "Backups", domain.EventJobDead: "Dead jobs",
}

// Told is what a webhook asking for a kind of event is told of: "A play starts".
func (w Words) Told(k domain.EventKind) string {
	return told[k]
}

var told = map[domain.EventKind]string{
	domain.EventPlaybackStarted: "A play starts", domain.EventPlaybackPaused: "A play is paused",
	domain.EventPlaybackResumed: "A play is resumed", domain.EventPlaybackStopped: "A play stops",
	domain.EventSignedIn: "Someone signs in", domain.EventSignInRefused: "A sign-in is refused",
	domain.EventProfileAdded: "A profile is added", domain.EventProfileRemoved: "A profile is removed",
	domain.EventLibraryAdded: "A library is added", domain.EventLibraryRemoved: "A library is removed",
	domain.EventLibraryScanned: "A library is scanned", domain.EventTitlesAdded: "Titles are added",
	domain.EventTaskFailed: "A task fails", domain.EventBackupMade: "A backup is made",
}

// Described is a value's name and what it means, in a sentence.
type Described struct {
	Name, Description string
}

// NodeRole is what a node does for the server, in a few words and in a sentence.
func (w Words) NodeRole(r domain.NodeRole) Described {
	return nodeRoles[r]
}

var nodeRoles = map[domain.NodeRole]Described{
	domain.NodeAll:       {"Serves and transcodes", "As every server on its own does."},
	domain.NodeTranscode: {"Transcodes first", "Asked to transcode before any other, while it has room: the server with the GPU. It serves people too."},
	domain.NodeServe:     {"Serves only", "Transcodes nothing, for playback or downloads, leaving that to the others."},
}

// TranscodeReason is why a copy is not played as it is: what of it, and that in a sentence a
// player can show.
func (w Words) TranscodeReason(r domain.TranscodeReason) Described {
	return transcodeReasons[r]
}

var transcodeReasons = map[domain.TranscodeReason]Described{
	domain.ContainerNotSupported:       {"Container", "The player doesn't open the file's container."},
	domain.VideoCodecNotSupported:      {"Video codec", "The player doesn't play the video's codec."},
	domain.VideoProfileNotSupported:    {"Video profile", "The player doesn't play the video's profile."},
	domain.VideoLevelNotSupported:      {"Video level", "The video's level is higher than the player plays."},
	domain.VideoResolutionNotSupported: {"Resolution", "The picture is larger than the player plays."},
	domain.VideoBitDepthNotSupported:   {"Bit depth", "The video's bit depth is more than the player plays."},
	domain.VideoRangeNotSupported:      {"HDR", "The screen doesn't show the video's HDR."},
	domain.AudioCodecNotSupported:      {"Audio codec", "The player doesn't play the audio's codec."},
	domain.AudioChannelsNotSupported:   {"Audio channels", "The audio has more channels than the player plays."},
	domain.BitrateExceedsLimit:         {"Bitrate", "The file is above the quality chosen."},
	domain.SubtitleCodecNotSupported:   {"Subtitles", "The subtitles are pictures, drawn into the video."},
	domain.PartsNotSupported:           {"Several files", "The title is in several files, played as one."},
}

// Sorted is a wall's order by name, and each way of it.
type Sorted struct {
	Name, Ascending, Descending string
}

// Sort is a wall's sort by name, and its two directions as a reader says them.
func (w Words) Sort(s domain.WallSort) Sorted {
	return sorts[s]
}

var sorts = map[domain.WallSort]Sorted{
	domain.SortTitle:    {"Title", "A to Z", "Z to A"},
	domain.SortAdded:    {"Date added", "Oldest first", "Newest first"},
	domain.SortReleased: {"Release date", "Oldest first", "Newest first"},
	domain.SortRating:   {"Rating", "Lowest first", "Highest first"},
	domain.SortRuntime:  {"Runtime", "Shortest first", "Longest first"},
	domain.SortPlayed:   {"Last played", "Longest ago", "Most recent"},
}
