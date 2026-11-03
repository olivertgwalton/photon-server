package artwork

import (
	"bytes"
	"io/fs"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// libraryPicture opens a picture a library holds: the picture file at rel, or the cover the sound
// file at rel holds, as Jellyfin's embedded image extractor reads one. The cover is read again
// each time, as the file's own pictures are; only its copies are kept.
func libraryPicture(root, rel string) (blob.Object, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return blob.Object{}, err
	}
	if !naming.IsAudio(rel) {
		return blob.OfFile(f)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return blob.Object{}, err
	}
	pic, err := media.Picture(f.Name())
	if err == nil && pic == nil {
		err = fs.ErrNotExist
	}
	if err != nil {
		return blob.Object{}, err
	}
	return blob.Object{ReadSeekCloser: inMemory{bytes.NewReader(pic)}, ModTime: info.ModTime()}, nil
}

type inMemory struct{ *bytes.Reader }

func (inMemory) Close() error { return nil }
