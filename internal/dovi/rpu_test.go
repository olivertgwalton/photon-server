package dovi

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

// The testdata is two seconds of a UHD Blu-ray's profile 7 FEL RPUs, the same made MEL by
// dovi_tool's mode 1, and what dovi_tool's mode 2 makes of each: Annex B, a start code before
// every NAL unit.
func nals(tb testing.TB, name string) [][]byte {
	tb.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		tb.Fatal(err)
	}
	var out [][]byte
	for part := range bytes.SplitSeq(b, []byte{0, 0, 0, 1}) {
		if len(part) > 0 {
			out = append(out, append([]byte{0x7c, 0x01}, part...))
		}
	}
	return out
}

func TestProfile81IsWhatDoviToolMakes(t *testing.T) {
	for _, layer := range []string{"fel", "mel"} {
		from, want := nals(t, layer+"7.rpu"), nals(t, layer+"81.rpu")
		if len(from) == 0 || len(from) != len(want) {
			t.Fatalf("%s: %d RPUs to convert, %d converted", layer, len(from), len(want))
		}
		for i := range from {
			got, err := Profile81(from[i])
			if err != nil {
				t.Fatalf("%s RPU %d: %v", layer, i, err)
			}
			if !bytes.Equal(got, bytes.TrimRight(want[i], "\x00")) {
				t.Fatalf("%s RPU %d:\n got %x\nwant %x", layer, i, got, want[i])
			}
		}
	}
}

func TestProfile81RefusesWhatIsNotProfile7(t *testing.T) {
	p81 := nals(t, "fel81.rpu")[0]
	if _, err := Profile81(p81); !errors.Is(err, ErrNotProfile7) {
		t.Errorf("an 8.1 RPU: %v, want ErrNotProfile7", err)
	}
	broken := bytes.Clone(nals(t, "fel7.rpu")[0])
	broken[20] ^= 0xff
	if _, err := Profile81(broken); err == nil {
		t.Error("an RPU failing its CRC was converted")
	}
}

// FuzzProfile81 fuzzes an RPU's body, between its type byte and its CRC, the CRC made to match so
// that the body is read.
func FuzzProfile81(f *testing.F) {
	for _, n := range nals(f, "fel7.rpu")[:4] {
		rpu := bytes.TrimRight(unescape(n[2:]), "\x00")
		f.Add(rpu[1 : len(rpu)-5])
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		rpu := append([]byte{0x19}, body...)
		crc := crc32MPEG2(body)
		rpu = append(rpu, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc), 0x80)
		got, err := Profile81(append([]byte{0x7c, 0x01}, escape(rpu)...))
		if err != nil {
			return
		}
		if _, err := Profile81(got); !errors.Is(err, ErrNotProfile7) {
			t.Fatalf("converting the converted RPU: %v, want ErrNotProfile7", err)
		}
	})
}
