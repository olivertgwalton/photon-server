package naming

import (
	"path"
	"slices"
	"strings"
)

var videoExtensions = []string{
	".3g2", ".3gp", ".amv", ".asf", ".avi", ".divx", ".dv", ".dvr-ms", ".f4v", ".flv", ".img",
	".iso", ".m2t", ".m2ts", ".m2v", ".m4v", ".mk3d", ".mkv", ".mov", ".mp4", ".mpe", ".mpeg",
	".mpg", ".mts", ".mxf", ".nsv", ".nuv", ".ogm", ".ogv", ".qt", ".rm", ".rmvb", ".tp", ".ts",
	".vob", ".webm", ".wmv", ".wtv", ".xvid",
}

var subtitleExtensions = []string{".ass", ".idx", ".mks", ".sami", ".smi", ".srt", ".ssa", ".sub", ".sup", ".vtt"}

func IsVideo(name string) bool {
	return slices.Contains(videoExtensions, strings.ToLower(path.Ext(name)))
}

func IsSubtitle(name string) bool {
	return slices.Contains(subtitleExtensions, strings.ToLower(path.Ext(name)))
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
