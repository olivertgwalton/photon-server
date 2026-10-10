package media

import (
	"net/url"
	"os"
	"strconv"
	"time"
)

// Input is the media a tool reads: a library file, which the tool opens by its path, or an address,
// http or https, the tool fetches itself.
type Input struct {
	// File is the library file opened; nil for media read from URL.
	File *os.File
	// URL is where the media is; nil for a file that is its own media.
	URL *url.URL
	// Name is the media's file name, as a player is told it.
	Name string
	// release ends what reading URL holds, as a relayed input's place at the relay.
	release func()
}

const (
	// remoteStall is how long a fetch of media at an address may go without a byte before the tool
	// gives up on it, as a network mount that stops answering is given up on.
	remoteStall = 30 * time.Second
	// remoteWholeRun is how long a tool may read all of the media at an address, whose size is not known
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
	// file: keeps a name that begins as an address would, http: or the like, from being read as one.
	return []string{"-protocol_whitelist", "file", "-i", "file:" + in.File.Name()}
}

func (in Input) Close() error {
	if in.release != nil {
		in.release()
	}
	if in.File == nil {
		return nil
	}
	return in.File.Close()
}

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
