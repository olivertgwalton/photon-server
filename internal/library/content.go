package library

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
)

const contentSample = 64 << 10

// ContentKey identifies a copy by its bytes rather than its path: the part count, then the first
// part's size and its first and last 64 KiB. A rename or a move keeps the key; replacing the file
// changes it.
func ContentKey(root *os.Root, parts []string) ([]byte, error) {
	f, err := root.Open(parts[0])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_ = binary.Write(h, binary.BigEndian, int64(len(parts)))
	_ = binary.Write(h, binary.BigEndian, info.Size())
	if _, err := io.CopyN(h, f, min(contentSample, info.Size())); err != nil {
		return nil, err
	}
	if tail := info.Size() - contentSample; tail > contentSample {
		if _, err := io.Copy(h, io.NewSectionReader(f, tail, contentSample)); err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}
