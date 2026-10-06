package domain

// ThemeLookup is where a library finds the tunes played under its titles' pages: files beside them
// alone, those and a show's theme from Plex's theme host where it has no file, as Plex's TV agent
// does by default, or none at all.
type ThemeLookup string

const (
	ThemesAll   ThemeLookup = "all"
	ThemesLocal ThemeLookup = "local"
	ThemesOff   ThemeLookup = "off"
)

func ThemeLookups() []ThemeLookup {
	return []ThemeLookup{ThemesAll, ThemesLocal, ThemesOff}
}

// Offers is whether a theme from source is played under it.
func (l ThemeLookup) Offers(source ThemeSource) bool {
	switch l {
	case ThemesAll:
		return true
	case ThemesLocal:
		return source == ThemeFromFile
	case ThemesOff:
		return false
	}
	panic("domain: unknown theme lookup: " + string(l))
}

// ThemeSource is where a theme tune came from: a file in the title's folder, or Plex's theme host.
type ThemeSource string

const (
	ThemeFromFile     ThemeSource = "file"
	ThemeFromTVThemes ThemeSource = "tvthemes"
)

func ThemeSources() []ThemeSource {
	return []ThemeSource{ThemeFromFile, ThemeFromTVThemes}
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
