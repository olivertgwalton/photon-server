package artwork

import (
	"bytes"
	"io/fs"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/ebook"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// libraryPicture opens a picture a library holds: the picture file at rel, or the cover the sound
// file or the ebook at rel holds, as Jellyfin's embedded image extractor reads one. The cover is
// read again each time, as the file's own pictures are; only its copies are kept.
func libraryPicture(root, rel string) (blob.Object, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return blob.Object{}, err
	}
	if !holdsPicture(rel) {
		return blob.OfFile(f)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return blob.Object{}, err
	}
	pic, err := heldPicture(f.Name())
	if err == nil && pic == nil {
		err = fs.ErrNotExist
	}
	if err != nil {
		return blob.Object{}, err
	}
	return blob.Object{ReadSeekCloser: inMemory{bytes.NewReader(pic)}, ModTime: info.ModTime()}, nil
}

// holdsPicture is whether a library's file at rel is one that holds its picture: a sound file or an
// ebook, not a picture.
func holdsPicture(rel string) bool {
	return naming.IsAudio(rel) || naming.IsBook(rel)
}

// heldPicture is the cover the sound file or the ebook at name holds; nil for none.
func heldPicture(name string) ([]byte, error) {
	if !naming.IsBook(name) {
		return media.Picture(name)
	}
	info, err := ebook.Read(name)
	if err != nil || info.Cover == "" {
		return nil, err
	}
	return ebook.Cover(name, info.Cover)
}

type inMemory struct{ *bytes.Reader }

func (inMemory) Close() error { return nil }
