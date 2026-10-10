package hls

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// start replaces the session's ffmpeg with one starting at segment n; the session's lock is held.
// The run outlives the request that asked for it.
func (r *Remuxer) start(ctx context.Context, s *session, n int) {
	if s.run != nil {
		s.run.cancel()
	}
	// The player is where it asked the run to start, wherever it had been: after a seek back the
	// run waits ahead of that, not ahead of the furthest it ever asked for.
	s.furthest = n
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &run{cancel: cancel, part: s.plan[n].Part, first: n, at: n, more: make(chan struct{}, 1)}
	s.run = run
	go func() {
		err := r.produce(ctx, s, run)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.run == run {
			s.run = nil
		}
		if errors.Is(err, errIdle) {
			return
		}
		// The file was read to its end, and the readers with it.
		if err == nil && run.at > run.first {
			if c := waiter(s.cued, run.at-1); !closed(c) {
				close(c)
			}
		}
		// A file shorter than it says ends before the part's last segments.
		if err == nil && run.at < len(s.plan) && s.plan[run.at].Part == run.part {
			err = fmt.Errorf("%w: segment %d", errEnded, run.at)
		}
		if err != nil && ctx.Err() == nil {
			r.log.WarnContext(ctx, "remux failed", slog.String("dir", s.dir), slog.Any("err", err))
		}
		// Whoever waits on a segment this run would have made learns it will not be.
		if err != nil && ctx.Err() == nil {
			if c, ok := s.inits[run.part]; ok && !closed(c) {
				close(c)
				delete(s.inits, run.part)
			}
			for m := run.at; m < len(s.plan) && s.plan[m].Part == run.part; m++ {
				if c, ok := s.ready[m]; ok && !closed(c) {
					s.failed[m] = err
					close(c)
				}
			}
			for m := max(run.first, run.at-1); m < len(s.plan) && s.plan[m].Part == run.part; m++ {
				if c, ok := s.cued[m]; ok && !closed(c) {
					s.cueFailed[m] = err
					close(c)
				}
			}
		}
	}()
}

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// produce runs ffmpeg from the start of segment run.at, cuts what it writes into the plan's
// segments and keeps each as it is finished.
func (r *Remuxer) produce(ctx context.Context, s *session, run *run) error {
	src := s.sources[run.part]
	in, err := src.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	start := s.plan[run.at].Start
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	var layer *styledLayer
	if src.Styled != nil {
		if layer, err = r.styled(ctx, s, *src.Styled, s.offsets[run.part]); err != nil {
			return err
		}
	}
	// Each embedded subtitle is written to a loopback socket of its own, read as ffmpeg reaches its
	// cues: a descriptor beyond stdout cannot be passed to a process on Windows.
	sockets, err := s.subtitleSockets(ctx)
	if err != nil {
		return err
	}
	// Once ffmpeg has ended, a reader still waiting for it to connect finds no cues.
	defer closeSockets(sockets)
	cmd := media.NewCommand(ctx, media.Foreground, r.tools.FFmpeg.Path, args(in, r.hw, start, src.Video, src.Audio, layer, s.format, sockets)...)
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return err
	}
	var readers errgroup.Group
	k := 0
	for _, sub := range s.subtitles {
		if sub.Stream == nil {
			continue
		}
		socket, offset := sockets[k], s.offsets[run.part]
		k++
		readers.Go(func() error {
			conn, err := socket.ln.Accept()
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if err != nil {
				return err
			}
			err = readVTT(conn, func(c Cue) {
				c.Start += offset
				c.End += offset
				c.Settings, c.Text = "", fromSubRip(c.Text)
				sub.add(c)
			})
			// What ffmpeg writes on is not read now, so it fails at once rather than blocking.
			return errors.Join(err, conn.Close())
		})
	}
	err = r.cut(ctx, s, run, out)
	if err != nil {
		stop(err)
	}
	// Nothing reads what ffmpeg writes now, so a write it is blocked on fails at once rather than
	// holding it past the grace a stop gives it.
	closeErr := out.Close()
	err = cmp.Or(err, closeErr, cmd.Err(cmd.Wait()))
	closeSockets(sockets)
	return cmp.Or(err, readers.Wait())
}

// subtitleSocket is a loopback socket ffmpeg writes a subtitle stream to.
type subtitleSocket struct {
	stream int
	ln     net.Listener
}

func (s subtitleSocket) url() string {
	return "tcp://" + s.ln.Addr().String()
}

// subtitleSockets listens on a loopback socket for each subtitle stream of the session.
func (s *session) subtitleSockets(ctx context.Context) ([]subtitleSocket, error) {
	var sockets []subtitleSocket
	var lc net.ListenConfig
	for _, sub := range s.subtitles {
		if sub.Stream == nil {
			continue
		}
		ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			closeSockets(sockets)
			return nil, err
		}
		sockets = append(sockets, subtitleSocket{stream: *sub.Stream, ln: ln})
	}
	return sockets, nil
}

// closeSockets stops listening; a socket closed twice says so, which is nothing to report.
func closeSockets(sockets []subtitleSocket) {
	for _, s := range sockets {
		s.ln.Close()
	}
}

// fragments is ffmpeg's output read as fragments that each begin on a video keyframe.
type fragments interface {
	next() (fragment, error)
	write(w io.Writer, f fragment) error
}

// cut reads ffmpeg's output and keeps the plan's segments of the run's part from run.at onwards.
// ffmpeg's seek lands on the keyframe at or before the one asked for, so fragments before the
// segment's start are dropped; every fragment starts on a keyframe, and so does every segment.
func (r *Remuxer) cut(ctx context.Context, s *session, run *run, out io.Reader) (err error) {
	st, err := s.fragments(run, out)
	if err != nil {
		return err
	}
	// A frame sits a few milliseconds off the keyframe index: both are rounded.
	const slack = 5 * time.Millisecond
	first := run.at
	n := first
	// The segment being written, under a name no player asks for until it is whole.
	var segment *os.File
	defer func() {
		if segment != nil {
			err = errors.Join(err, segment.Close(), s.root.Remove(segmentName(s.format, n)+".part"))
		}
	}()
	finish := func() error {
		err := segment.Close()
		segment = nil
		if err == nil {
			err = s.root.Rename(segmentName(s.format, n)+".part", segmentName(s.format, n))
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		if c := waiter(s.ready, n); !closed(c) {
			close(c)
		}
		// ffmpeg wrote the fragment that finished this segment once it had read past it, and a
		// subtitle's cues sit in the file where they are shown, so every cue shown before the
		// segment ends has been read. The previous segment's are taken as whole, not this one's,
		// leaving a segment's time for the threads ffmpeg writes each output on to keep up.
		if n > run.first {
			if c := waiter(s.cued, n-1); !closed(c) {
				close(c)
			}
		}
		n++
		run.at = n
		s.mu.Unlock()
		return nil
	}
	for {
		if err := r.throttle(ctx, s, run, n); err != nil {
			return err
		}
		frag, err := st.next()
		if errors.Is(err, io.EOF) {
			if segment != nil {
				return finish()
			}
			return nil
		}
		if err != nil {
			return err
		}
		if n == first && segment == nil && frag.shown < s.plan[first].Start-slack {
			if err := st.write(io.Discard, frag); err != nil {
				return err
			}
			continue
		}
		for n < len(s.plan) && s.plan[n].Part == run.part && frag.shown >= s.plan[n].End-slack && segment != nil {
			if err := finish(); err != nil {
				return err
			}
		}
		if n >= len(s.plan) || s.plan[n].Part != run.part {
			return nil
		}
		if segment == nil {
			if segment, err = s.root.Create(segmentName(s.format, n) + ".part"); err != nil {
				return err
			}
		}
		if err := st.write(segment, frag); err != nil {
			return err
		}
	}
}

// fragments reads ffmpeg's output in the session's format, keeping a fragmented MP4's
// initialisation as the part's where it is not yet.
func (s *session) fragments(run *run, out io.Reader) (fragments, error) {
	switch s.format {
	case domain.SegmentsMPEGTS:
		return readTS(out, s.plan[run.at].Start), nil
	case domain.SegmentsFMP4:
	}
	fmp4, init, err := readInit(out)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := waiter(s.inits, run.part); !closed(c) {
		if err := keep(s.root, initName(run.part), init); err != nil {
			return nil, err
		}
		close(c)
	}
	return fmp4, nil
}

// throttle waits while the run is far enough ahead of what has been asked for, and gives up with
// errIdle once nothing has been asked for r.idle.
func (r *Remuxer) throttle(ctx context.Context, s *session, run *run, n int) error {
	lead := ahead
	if s.encodes() {
		lead = aheadEncoding
	}
	for {
		s.mu.Lock()
		far := n > s.furthest+lead
		s.mu.Unlock()
		// A run replaced stops at its next fragment, rather than writing on beside the run that
		// replaced it.
		if !far {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.more:
		case <-time.After(r.idle):
			return errIdle
		}
	}
}

// keep writes a file whole before anyone can read it.
func keep(root *os.Root, name string, data []byte) error {
	part := name + ".part"
	if err := root.WriteFile(part, data, 0o600); err != nil {
		return err
	}
	return root.Rename(part, name)
}
