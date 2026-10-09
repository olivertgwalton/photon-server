package media

import (
	"os"
	"time"
)

// Input is the media a tool reads: a library file, passed to it as descriptor 3.
type Input struct {
	File *os.File
}

// Args open the input: the only protocol it may read through, and -i. Options of the input, a
// seek among them, go before them.
func (in Input) Args() []string {
	return []string{"-protocol_whitelist", "fd", "-fd", "3", "-i", "fd:"}
}

// Files are the descriptors a tool reading in is passed, from 3 up.
func (in Input) Files() []*os.File { return []*os.File{in.File} }

func (in Input) Close() error { return in.File.Close() }

// WholeRun is how long a tool reading all of in may run.
func (in Input) WholeRun() time.Duration {
	info, err := in.File.Stat()
	if err != nil {
		return PartRun
	}
	return PartRun + time.Duration(info.Size()/slowestRead)*time.Second
}
