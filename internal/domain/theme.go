package domain

// ThemeLookup is where a library finds the tunes played under its titles' pages: files beside them
// alone, those and, for a film or show with none, the one ThemerrDB lists, fetched from YouTube as
// Jellyfin's Themerr plugin does, or none at all.
type ThemeLookup string

const (
	ThemesLocal   ThemeLookup = "local"
	ThemesThemerr ThemeLookup = "themerr"
	ThemesOff     ThemeLookup = "off"
)

func ThemeLookups() []ThemeLookup {
	return []ThemeLookup{ThemesLocal, ThemesThemerr, ThemesOff}
}

// ThemeSource is where a theme tune came from: a file in the title's folder, or ThemerrDB's
// YouTube link.
type ThemeSource string

const (
	ThemeFromFile    ThemeSource = "file"
	ThemeFromThemerr ThemeSource = "themerr"
)

func ThemeSources() []ThemeSource {
	return []ThemeSource{ThemeFromFile, ThemeFromThemerr}
}

// ThemeMusic is whether a profile's pages play their title's theme tune, as Jellyfin's
// EnableThemeSongs, off unless asked for.
type ThemeMusic string

const (
	ThemeMusicPlay ThemeMusic = "play"
	ThemeMusicOff  ThemeMusic = "off"
)

func ThemeMusics() []ThemeMusic {
	return []ThemeMusic{ThemeMusicPlay, ThemeMusicOff}
}
