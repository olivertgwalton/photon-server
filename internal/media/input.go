package media

import (
	"net/url"
	"os"
	"strconv"
	"time"
)

// Input is the media a tool reads: a library file, passed to it as descriptor 3, or, for a .strm,
// the address the file names, which the tool fetches itself.
type Input struct {
	// File is the library file opened. It is passed for a .strm too, so whatever else a tool is
	// passed is numbered as for any file, but it cannot read it.
	File *os.File
	// URL is where a .strm's media is, http or https; nil for a file that is its own media.
	URL *url.URL
}

const (
	// remoteStall is how long a fetch of a .strm's media may go without a byte before the tool
	// gives up on it, as a network mount that stops answering is given up on.
	remoteStall = 30 * time.Second
	// remoteWholeRun is how long a tool may read all of a .strm's media, whose size is not known
	// before it is read. A fetch that stalls fails sooner; this ends one that never does, as a
	// live stream's would not.
	remoteWholeRun = 6 * time.Hour
)

// Args open the input: the only protocols it may be read through, and -i. Options of the input,
// a seek among them, go before them.
func (in Input) Args() []string {
	if in.URL != nil {
		return []string{
			"-protocol_whitelist", "http,https,tcp,tls", "-rw_timeout", strconv.FormatInt(remoteStall.Microseconds(), 10),
			"-i", in.URL.String(),
		}
	}
	return []string{"-protocol_whitelist", "fd", "-fd", "3", "-i", "fd:"}
}

// Files are the descriptors a tool reading in is passed, from 3 up.
func (in Input) Files() []*os.File { return []*os.File{in.File} }

func (in Input) Close() error { return in.File.Close() }

// WholeRun is how long a tool reading all of in may run.
func (in Input) WholeRun() time.Duration {
	if in.URL != nil {
		return remoteWholeRun
	}
	info, err := in.File.Stat()
	if err != nil {
		return PartRun
	}
	return PartRun + time.Duration(info.Size()/slowestRead)*time.Second
}
