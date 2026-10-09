package naming

import (
	"path"
	"slices"
	"strings"
)

var videoExtensions = []string{
	".3g2", ".3gp", ".amv", ".asf", ".avi", ".divx", ".dv", ".dvr-ms", ".f4v", ".flv", ".img",
	".iso", ".m2t", ".m2ts", ".m2v", ".m4v", ".mk3d", ".mkv", ".mov", ".mp4", ".mpe", ".mpeg",
	".mpg", ".mts", ".mxf", ".nsv", ".nuv", ".ogm", ".ogv", ".qt", ".rm", ".rmvb", ".strm", ".tp",
	".ts", ".vob", ".webm", ".wmv", ".wtv", ".xvid",
}

var subtitleExtensions = []string{".ass", ".idx", ".mks", ".sami", ".smi", ".srt", ".ssa", ".sub", ".sup", ".vtt"}

// audioTypes are the sound files a theme tune may be, as Jellyfin reads them, and the content type
// each is served as.
var audioTypes = map[string]string{
	".aac": "audio/aac", ".flac": "audio/flac", ".m4a": "audio/mp4", ".mp3": "audio/mpeg",
	".oga": "audio/ogg", ".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
	".wma": "audio/x-ms-wma",
}

// AudioType answers the content type of a sound file by its name, and false for any other file.
func AudioType(name string) (string, bool) {
	t, ok := audioTypes[strings.ToLower(path.Ext(name))]
	return t, ok
}

// Theme reports whether a file of a title's folder, named relative to it, is one of its theme
// tunes, as Jellyfin and Plex find them: theme.mp3 or another sound file named theme beside it, or
// any in its theme-music folder.
func Theme(name string) bool {
	if _, ok := AudioType(name); !ok {
		return false
	}
	dir, file := path.Split(name)
	return (dir == "" && strings.EqualFold(strings.TrimSuffix(file, path.Ext(file)), "theme")) || ThemeFolder(path.Clean(dir))
}

func IsVideo(name string) bool { return hasExt(videoExtensions, name) }

func IsSubtitle(name string) bool { return hasExt(subtitleExtensions, name) }

// IsShortcut reports whether a video file is a .strm, as Kodi, Jellyfin and Emby read one: a text
// file naming the address its media is at.
func IsShortcut(name string) bool { return strings.EqualFold(path.Ext(name), ".strm") }

func hasExt(list []string, name string) bool {
	return slices.Contains(list, strings.ToLower(path.Ext(name)))
}

// ignoredNames are folders and files a library never holds titles in: NAS and OS housekeeping,
// and other media servers' artwork folders.
var ignoredNames = []string{
	"@eadir", "eadir", "#recycle", "@recycle", ".@__thumb", "$recycle.bin",
	"system volume information", "lost+found", "metadata", "extrafanart", "extrathumbs",
	"ps3_update", "ps3_vprm", "temprec", "tempsbe", "thumbs.db",
}

// Ignored reports whether a file or folder name is never part of a library. Dot-files cover
// .DS_Store, AppleDouble's ._ files and .snapshot folders.
func Ignored(name string) bool {
	return strings.HasPrefix(name, ".") || slices.Contains(ignoredNames, strings.ToLower(name))
}

// SubtitleFolder reports whether a folder beside a video holds its subtitles, as Plex reads them.
func SubtitleFolder(name string) bool {
	n := strings.ToLower(name)
	return n == "subs" || n == "subtitles"
}

// ThemeFolder reports whether a folder beside a title holds its theme tunes, as Jellyfin reads them.
func ThemeFolder(name string) bool {
	return strings.EqualFold(name, "theme-music")
}

// FoldedFolder answers, for a folder whose files are its parent's (a Subs folder, a theme-music
// folder), which of them it holds, and false for any other folder.
func FoldedFolder(name string) (holds func(string) bool, ok bool) {
	switch {
	case SubtitleFolder(name):
		return IsSubtitle, true
	case ThemeFolder(name):
		return func(n string) bool { _, ok := AudioType(n); return ok }, true
	}
	return nil, false
}
