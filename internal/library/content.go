package library

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const contentSample = 64 << 10

// ContentKey identifies a copy by its bytes rather than its path: the part count, then the first
// part's size and its first and last 64 KiB. A rename or a move keeps the key; replacing the file
// changes it.
func ContentKey(root string, parts []string) ([]byte, error) {
	f, err := Open(root, parts[0])
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

// MovieHash is OpenSubtitles' hash of a file, by which a subtitle made for that very release is
// found: its size and every little-endian 64-bit word of its first and last 64 KiB, summed and
// let wrap, as 16 hex digits. A file shorter than that has none.
func MovieHash(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size < contentSample {
		return "", nil
	}
	sum := uint64(size)
	buf := make([]byte, contentSample)
	for _, at := range []int64{0, size - contentSample} {
		if _, err := f.ReadAt(buf, at); err != nil {
			return "", err
		}
		for i := 0; i < contentSample; i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}
