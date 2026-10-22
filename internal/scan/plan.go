package scan

import (
	"path"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// film is a title a folder holds, with every version of it.
type film struct {
	name     naming.Name
	versions []versionPlan
	// nfos are the names its NFO may have, in the order Jellyfin looks for them.
	nfos []string
}

// versionPlan is one version: its files in play order, and what tells it apart from its siblings.
type versionPlan struct {
	parts     []library.File
	edition   string
	label     string
	subtitles []subtitlePlan
}

type subtitlePlan struct {
	file  library.File
	codec string
	tags  naming.Subtitle
}

func stem(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// planFilms reads one folder of a movies library. A folder holding one film is named by the
// folder; so are several files that each start with the folder's name, which are versions of that
// film (Jellyfin's rule). Anything else, and every file at the library root, is a film of its own
// named by its file.
func planFilms(f library.Folder) []film {
	if _, isExtras := naming.ExtraFolder(path.Base(f.Path)); isExtras || naming.SampleFolder(path.Base(f.Path)) {
		return nil
	}
	stacked := stacks(f.Files)
	if len(stacked) == 0 {
		return nil
	}
	if f.Path != "." {
		folder := path.Base(f.Path)
		if v, ok := versionsOf(folder, stacked); ok {
			for i := range v {
				v[i].subtitles = subtitlesFor(f.Files, versionStem(v[i].parts), strings.TrimSpace(naming.StripTags(folder)), len(v) == 1)
			}
			return []film{{name: naming.CleanName(folder), versions: v, nfos: []string{nfoOf(v[0].parts), "movie.nfo"}}}
		}
	}
	films := make([]film, 0, len(stacked))
	for _, c := range stacked {
		n := naming.CleanName(stem(c[0].Name))
		subs := subtitlesFor(f.Files, versionStem(c), "", len(stacked) == 1)
		films = append(films, film{
			name: n, versions: []versionPlan{{parts: c, edition: n.Edition, subtitles: subs}}, nfos: []string{nfoOf(c)},
		})
	}
	return films
}

// nfoOf is the NFO named after a version's first file.
func nfoOf(parts []library.File) string {
	return stem(parts[0].Name) + ".nfo"
}

// versionStem is the name a version's subtitles are named after: its file's stem, or for a version split
// across files what its parts share.
func versionStem(parts []library.File) string {
	s := stem(parts[0].Name)
	if p, ok := naming.StackPart(s); ok {
		return strings.TrimRight(p.Base, " ._-")
	}
	return s
}

// subtitlesFor picks a version's subtitle files from its folder's, Subs folders included. A subtitle
// named after the version ("Heat (1995) - 2160p.en.srt") is the version's; one named after the title
// ("Heat (1995).en.srt") is every version's, which Jellyfin leaves unattached. In a Subs folder
// beside a single version, a subtitle named otherwise ("Subs/English.srt", as releases ship them) is
// that version's too.
func subtitlesFor(files []library.File, versionName, titleName string, onlyVersion bool) []subtitlePlan {
	var subs []subtitlePlan
	for _, f := range files {
		codec, ok := naming.SubtitleCodec(f.Name)
		if !ok {
			continue
		}
		s := stem(path.Base(f.Name))
		tags, matched := "", false
		for _, name := range []string{versionName, titleName} {
			if name == "" {
				continue
			}
			if rest, ok := cutPrefixFold(s, name); ok && (rest == "" || rest[0] == '.') {
				tags, matched = rest, true
				break
			}
		}
		if !matched && onlyVersion && strings.Contains(f.Name, "/") {
			tags, matched = "."+s, true
		}
		if matched {
			subs = append(subs, subtitlePlan{file: f, codec: codec, tags: naming.SubtitleTags(tags)})
		}
	}
	return subs
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// stacks groups a folder's videos into versions: a file on its own, or the parts of one version split
// across files with the same base and marker.
func stacks(files []library.File) [][]library.File {
	type key struct{ base, marker string }
	var versions [][]library.File
	stacked := map[key]int{}
	for _, f := range files {
		if !naming.IsVideo(f.Name) {
			continue
		}
		if _, _, extra := naming.Extra(stem(f.Name)); extra || naming.Sample(stem(f.Name)) {
			continue
		}
		if p, ok := naming.StackPart(stem(f.Name)); ok {
			k := key{strings.ToLower(p.Base), p.Marker}
			if i, seen := stacked[k]; seen {
				versions[i] = append(versions[i], f)
				continue
			}
			stacked[k] = len(versions)
		}
		versions = append(versions, []library.File{f})
	}
	for _, c := range versions {
		slices.SortFunc(c, func(a, b library.File) int {
			pa, _ := naming.StackPart(stem(a.Name))
			pb, _ := naming.StackPart(stem(b.Name))
			return pa.Number - pb.Number
		})
	}
	return versions
}

// versionsOf groups a folder's stacks of files as versions of its film. One stack is the folder's film
// whatever its file is called. Several are its versions only when every name gives the same year
// and begins with the folder's (without its id and edition tags), and what follows is nothing, a
// separator or a bracket, which becomes the version's label. That is Jellyfin's rule, except that
// a brace or parenthesis also opens a label, for Plex's edition tags.
func versionsOf(folder string, stacked [][]library.File) ([]versionPlan, bool) {
	folder = strings.TrimSpace(naming.StripTags(folder))
	plans := make([]versionPlan, 0, len(stacked))
	for _, c := range stacked {
		s := stem(c[0].Name)
		if p, ok := naming.StackPart(s); ok {
			s = p.Base
		}
		if naming.CleanName(s).Year != naming.CleanName(stem(stacked[0][0].Name)).Year {
			return nil, false
		}
		rest, prefixed := cutPrefixFold(s, folder)
		if prefixed {
			rest = strings.TrimSpace(rest)
			if rest != "" && !strings.ContainsRune("-_.[{(", rune(rest[0])) {
				rest = ""
				if len(stacked) > 1 {
					return nil, false
				}
			}
		} else if len(stacked) > 1 {
			return nil, false
		}
		plans = append(plans, versionPlan{
			parts:   c,
			edition: naming.CleanName(s).Edition,
			label:   strings.Trim(naming.StripTags(rest), " -_.()[]"),
		})
	}
	return plans, true
}
