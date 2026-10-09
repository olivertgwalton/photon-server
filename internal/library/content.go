package library

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const contentSample = 64 << 10

// ContentKey identifies a copy of parts parts by its bytes rather than its path: the part count,
// then its first part's size and first and last 64 KiB, first being that part open. A rename or a
// move keeps the key; replacing the file changes it. It reads without moving the file's offset.
func ContentKey(first *os.File, parts int) ([]byte, error) {
	info, err := first.Stat()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(binary.BigEndian.AppendUint64(nil, uint64(parts)))
	h.Write(binary.BigEndian.AppendUint64(nil, uint64(info.Size())))
	if _, err := io.Copy(h, io.NewSectionReader(first, 0, min(contentSample, info.Size()))); err != nil {
		return nil, err
	}
	if tail := info.Size() - contentSample; tail > contentSample {
		if _, err := io.Copy(h, io.NewSectionReader(first, tail, contentSample)); err != nil {
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
