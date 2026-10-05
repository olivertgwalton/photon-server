package scan

import (
	"path"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// extraPlan is an extra a folder holds and whose it is: an extras folder's videos belong to the
// title of the folder above it; an extra named after a title ("Heat (1995)-trailer") to that
// title in its own folder.
type extraPlan struct {
	kind  domain.ExtraKind
	title string
	copy  copyPlan
	// ownerName is the title the extra's name gives, empty for one in an extras folder.
	ownerName string
}

// extrasIn reads a folder's extras: every video of an extras folder, or the suffixed extras
// beside titles in any other folder.
func extrasIn(f library.Folder) (extras []extraPlan, inExtrasFolder bool) {
	folderKind, inExtrasFolder := naming.ExtraFolder(path.Base(f.Path))
	for _, file := range f.Files {
		if !naming.IsVideo(file.Name) || naming.Sample(stem(file.Name)) {
			continue
		}
		s := stem(file.Name)
		kind, owner, suffixed := naming.Extra(s)
		switch {
		case inExtrasFolder:
			if !suffixed {
				kind = folderKind
			}
			extras = append(extras, extraPlan{kind: kind, title: naming.CleanName(s).Title, copy: copyPlan{parts: []library.File{file}}})
		case suffixed:
			extras = append(extras, extraPlan{kind: kind, title: kindTitle[kind], ownerName: owner, copy: copyPlan{parts: []library.File{file}}})
		}
	}
	return extras, inExtrasFolder
}

// kindTitle names an extra whose file names only its title and its kind ("Heat (1995)-trailer").
var kindTitle = map[domain.ExtraKind]string{
	domain.ExtraTrailer: "Trailer", domain.ExtraFeaturette: "Featurette",
	domain.ExtraBehindTheScenes: "Behind the Scenes", domain.ExtraDeletedScene: "Deleted Scene",
	domain.ExtraInterview: "Interview", domain.ExtraScene: "Scene", domain.ExtraShort: "Short",
	domain.ExtraClip: "Clip", domain.ExtraThemeVideo: "Theme Video", domain.ExtraOther: "Extra",
}
