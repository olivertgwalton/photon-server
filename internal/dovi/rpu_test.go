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
func nals(t *testing.T, name string) [][]byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
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
