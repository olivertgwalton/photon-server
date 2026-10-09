package library

import "github.com/olivertgwalton/photon-server/internal/media"

// OpenMedia opens a file of the library at root, as Open does, for a tool to read.
func OpenMedia(root, rel string) (media.Input, error) {
	f, err := Open(root, rel)
	return media.Input{File: f}, err
}
