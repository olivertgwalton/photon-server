package scan

import (
	"path"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// film is a title a folder holds, with every copy of it.
type film struct {
	name     naming.Name
	versions []copyPlan
}

// copyPlan is one copy: its files in play order, and what tells it apart from its siblings.
type copyPlan struct {
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
// folder; so are several files that each start with the folder's name, which are copies of that
// film (Jellyfin's rule). Anything else, and every file at the library root, is a film of its own
// named by its file.
func planFilms(f library.Folder) []film {
	if _, isExtras := naming.ExtraFolder(path.Base(f.Path)); isExtras {
		return nil
	}
	copies := stacks(f.Files)
	if len(copies) == 0 {
		return nil
	}
	if f.Path != "." {
		folder := path.Base(f.Path)
		if v, ok := versionsOf(folder, copies); ok {
			for i := range v {
				v[i].subtitles = subtitlesFor(f.Files, copyStem(v[i].parts), strings.TrimSpace(naming.StripTags(folder)), len(v) == 1)
			}
			return []film{{name: naming.CleanName(folder), versions: v}}
		}
	}
	films := make([]film, 0, len(copies))
	for _, c := range copies {
		n := naming.CleanName(stem(c[0].Name))
		subs := subtitlesFor(f.Files, copyStem(c), "", len(copies) == 1)
		films = append(films, film{name: n, versions: []copyPlan{{parts: c, edition: n.Edition, subtitles: subs}}})
	}
	return films
}

// copyStem is the name a copy's subtitles are named after: its file's stem, or for a copy split
// across files what its parts share.
func copyStem(parts []library.File) string {
	s := stem(parts[0].Name)
	if p, ok := naming.StackPart(s); ok {
		return strings.TrimRight(p.Base, " ._-")
	}
	return s
}

// subtitlesFor picks a copy's subtitle files from its folder's, Subs folders included. A subtitle
// named after the copy ("Heat (1995) - 2160p.en.srt") is the copy's; one named after the title
// ("Heat (1995).en.srt") is every version's, which Jellyfin leaves unattached. In a Subs folder
// beside a single copy, a subtitle named otherwise ("Subs/English.srt", as releases ship them) is
// that copy's too.
func subtitlesFor(files []library.File, copyName, titleName string, onlyCopy bool) []subtitlePlan {
	var subs []subtitlePlan
	for _, f := range files {
		codec, ok := naming.SubtitleCodec(f.Name)
		if !ok {
			continue
		}
		s := stem(path.Base(f.Name))
		tags, matched := "", false
		for _, name := range []string{copyName, titleName} {
			if name == "" {
				continue
			}
			if rest, ok := cutPrefixFold(s, name); ok && (rest == "" || rest[0] == '.') {
				tags, matched = rest, true
				break
			}
		}
		if !matched && onlyCopy && strings.Contains(f.Name, "/") {
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

// stacks groups a folder's videos into copies: a file on its own, or the parts of one copy split
// across files with the same base and marker.
func stacks(files []library.File) [][]library.File {
	type key struct{ base, marker string }
	var copies [][]library.File
	stacked := map[key]int{}
	for _, f := range files {
		if !naming.IsVideo(f.Name) {
			continue
		}
		if _, extra := naming.Extra(stem(f.Name)); extra {
			continue
		}
		if p, ok := naming.StackPart(stem(f.Name)); ok {
			k := key{strings.ToLower(p.Base), p.Marker}
			if i, seen := stacked[k]; seen {
				copies[i] = append(copies[i], f)
				continue
			}
			stacked[k] = len(copies)
		}
		copies = append(copies, []library.File{f})
	}
	for _, c := range copies {
		slices.SortFunc(c, func(a, b library.File) int {
			pa, _ := naming.StackPart(stem(a.Name))
			pb, _ := naming.StackPart(stem(b.Name))
			return pa.Number - pb.Number
		})
	}
	return copies
}

// versionsOf groups copies as versions of the folder's film. One copy is the folder's film
// whatever its file is called. Several are its versions only when each name begins with the
// folder's (without its id and edition tags) and what follows is nothing, a separator or a
// bracket, which becomes the version's label (Jellyfin's rule).
func versionsOf(folder string, copies [][]library.File) ([]copyPlan, bool) {
	folder = strings.TrimSpace(naming.StripTags(folder))
	plans := make([]copyPlan, 0, len(copies))
	for _, c := range copies {
		s := stem(c[0].Name)
		if p, ok := naming.StackPart(s); ok {
			s = p.Base
		}
		rest, prefixed := cutPrefixFold(s, folder)
		if prefixed {
			rest = strings.TrimSpace(rest)
			if rest != "" && !strings.ContainsRune("-_.[{(", rune(rest[0])) {
				rest = ""
				if len(copies) > 1 {
					return nil, false
				}
			}
		} else if len(copies) > 1 {
			return nil, false
		}
		plans = append(plans, copyPlan{
			parts:   c,
			edition: naming.CleanName(s).Edition,
			label:   strings.Trim(naming.StripTags(rest), " -_.()[]"),
		})
	}
	return plans, true
}
