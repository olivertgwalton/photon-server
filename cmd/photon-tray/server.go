//go:build windows

package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// stateKind is where the server is: starting, running, stopped, or failed with a reason.
type stateKind string

const (
	starting stateKind = "starting"
	running  stateKind = "running"
	stopped  stateKind = "stopped"
	failed   stateKind = "failed"
)

type state struct {
	kind stateKind
	why  string
}

func (s state) String() string {
	switch s.kind {
	case starting:
		return "Starting…"
	case running:
		return "Running"
	case stopped:
		return "Stopped"
	case failed:
		return "Stopped: " + s.why
	}
	return string(s.kind)
}

const (
	address = "http://localhost:8640"
	// postgresMajor is the PostgreSQL the app carries: a database another made is refused rather
	// than started.
	postgresMajor = "18"
	// exitRestart is the status photon-server exits with when it is stopped for a restore, to be
	// started again.
	exitRestart = 75
	// valkeyPort is where the Valkey the app carries listens, on loopback alone: MSYS2's Unix
	// sockets are its own, which Go cannot reach.
	valkeyPort = 8641
	// stopWithin is how long a process told to stop has before it is ended.
	stopWithin = 30 * time.Second
)

// server runs photon-server from the install, as the release archive lays it out, beside the
// PostgreSQL and Valkey the app carries. PostgreSQL listens on a socket in the user's own folder
// in AppData, which only this user can reach, so it needs no password or port; Valkey listens on
// loopback with a password made each time it starts. It makes the database the first time,
// migrates it, serves until stopped, and starts photon-server again when it exits 75, as it does
// after a restore.
type server struct {
	app, home string
	logger    *slog.Logger
	changed   func()

	// life is held while the services start and while they stop, so a stop waits for a start.
	life     sync.Mutex
	mu       sync.Mutex
	state    state
	stopping bool
	valkey   *exec.Cmd
	vexited  chan struct{}
	password string
	photon   *exec.Cmd
	exited   chan struct{}
}

func newServer(app, home string, logger *slog.Logger, changed func()) *server {
	return &server{app: app, home: home, logger: logger, changed: changed, state: state{kind: stopped}}
}

func (s *server) logs() string     { return filepath.Join(s.home, "logs") }
func (s *server) data() string     { return filepath.Join(s.home, "postgres") }
func (s *server) postgres() string { return filepath.Join(s.app, "postgres", "bin") }

func (s *server) current() state {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *server) set(st state) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	s.changed()
}

func (s *server) fail(format string, args ...any) {
	s.set(state{kind: failed, why: fmt.Sprintf(format, args...)})
}

func (s *server) start() {
	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()
	s.set(state{kind: starting})
	go func() {
		s.life.Lock()
		env, err := s.startServices()
		s.life.Unlock()
		if err != nil {
			s.fail("%v", err)
			return
		}
		s.serve(env)
	}()
}

// stop stops photon-server at once, told twice to play no stream to its end, then Valkey and
// PostgreSQL.
func (s *server) stop() {
	s.life.Lock()
	defer s.life.Unlock()
	s.mu.Lock()
	s.stopping = true
	photon, exited, valkey, vexited, password := s.photon, s.exited, s.valkey, s.vexited, s.password
	s.mu.Unlock()
	if photon != nil {
		pid := photon.Process.Pid
		for range 2 {
			if err := interrupt(pid); err != nil {
				s.logger.Error("photon-server not told to stop", slog.Any("err", err))
			}
		}
		s.await("photon-server", photon, exited)
	}
	if valkey != nil {
		s.shutdownValkey(valkey, vexited, password)
	}
	if err := s.tool("pg_ctl", "-D", s.data(), "-m", "fast", "-w", "stop"); err != nil {
		s.logger.Error("postgres not stopped", slog.Any("err", err))
	}
	s.mu.Lock()
	s.photon, s.valkey = nil, nil
	s.mu.Unlock()
	s.set(state{kind: stopped})
}

// await waits for a process told to stop, and ends it once it has had stopWithin.
func (s *server) await(name string, p *exec.Cmd, exited <-chan struct{}) {
	t := time.NewTimer(stopWithin)
	defer t.Stop()
	select {
	case <-exited:
	case <-t.C:
		s.logger.Error("not stopped when told; ended", slog.String("process", name))
		if err := p.Process.Kill(); err != nil {
			s.logger.Error("not ended", slog.String("process", name), slog.Any("err", err))
			return
		}
		<-exited
	}
}

// interrupt sends photon-server, in a process group of its own, a Ctrl+Break on its console,
// which Go delivers as os.Interrupt: Windows has no SIGTERM. The tray has no console, so it
// borrows the server's to send it.
func interrupt(pid int) error {
	if r, _, err := attachConsole.Call(uintptr(pid)); r == 0 {
		return fmt.Errorf("attach to its console: %w", err)
	}
	defer freeConsole.Call()
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid))
}

// shutdownValkey asks Valkey to save and stop, so sessions live across a restart.
func (s *server) shutdownValkey(p *exec.Cmd, exited <-chan struct{}, password string) {
	if c, err := dialValkey(); err == nil {
		_, err = fmt.Fprintf(c, "AUTH %s\r\nSHUTDOWN\r\n", password)
		if err := errors.Join(err, c.Close()); err != nil {
			s.logger.Error("valkey-server not told to stop", slog.Any("err", err))
		}
	}
	s.await("valkey-server", p, exited)
}

func dialValkey() (net.Conn, error) {
	d := net.Dialer{Timeout: time.Second}
	return d.DialContext(context.Background(), "tcp", valkeyAddress())
}

func valkeyAddress() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(valkeyPort)) }

func (s *server) serve(env []string) {
	for {
		code, err := s.photonServer(env, "migrate")
		if s.isStopping() {
			return
		}
		if err != nil || code != 0 {
			s.fail("the database was not migrated (%d): see the log", code)
			return
		}
		s.set(state{kind: running})
		code, err = s.photonServer(env)
		if s.isStopping() {
			return
		}
		switch {
		case err != nil:
			s.fail("photon-server did not start: %v", err)
			return
		case code == exitRestart:
			continue
		case code == 0:
			s.set(state{kind: stopped})
			return
		default:
			s.fail("photon-server exited %d: see the log", code)
			return
		}
	}
}

func (s *server) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

// photonServer runs photon-server with args to its end, in a process group of its own so it
// alone is interrupted, and answers its exit code.
func (s *server) photonServer(env []string, args ...string) (int, error) {
	log, err := s.logFile("photon-server.log")
	if err != nil {
		return 0, err
	}
	defer log.Close()
	p := exec.CommandContext(context.Background(), filepath.Join(s.app, "photon-server", "bin", "photon-server.exe"), args...) //nolint:gosec // the install's own server
	p.Env = env
	p.Stdout, p.Stderr = log, log
	p.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
	if err := p.Start(); err != nil {
		return 0, err
	}
	exited := make(chan struct{})
	s.mu.Lock()
	s.photon, s.exited = p, exited
	if s.stopping {
		// Told to stop while it started, after stop looked for it: Wait answers either way.
		p.Process.Kill() //nolint:errcheck // as said
	}
	s.mu.Unlock()
	err = p.Wait()
	close(exited)
	code := p.ProcessState.ExitCode()
	s.logger.Info("photon-server exited", slog.String("command", cmp.Or(strings.Join(args, " "), "serve")), slog.Int("exit", code))
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		err = nil
	}
	return code, err
}

// startServices makes the database the first time, starts PostgreSQL and then Valkey, and
// answers photon-server's environment once Valkey takes connections.
func (s *server) startServices() ([]string, error) {
	if err := os.MkdirAll(s.logs(), 0o700); err != nil {
		return nil, err
	}
	version := filepath.Join(s.data(), "PG_VERSION")
	if _, err := os.Stat(version); errors.Is(err, os.ErrNotExist) {
		if err := s.initdb(); err != nil {
			return nil, err
		}
	}
	made, err := os.ReadFile(version)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(string(made)); v != postgresMajor {
		return nil, fmt.Errorf("the database was made by PostgreSQL %s, not %s", v, postgresMajor)
	}
	if err := s.tool("pg_ctl", "-D", s.data(), "-l", filepath.Join(s.logs(), "postgres.log"), "-w", "start"); err != nil {
		return nil, err
	}
	exists, err := s.query("SELECT 1 FROM pg_database WHERE datname = 'photon'")
	if err != nil {
		return nil, err
	}
	if exists == "" {
		if _, err := s.query("CREATE DATABASE photon"); err != nil {
			return nil, err
		}
	}
	if err := s.startValkey(); err != nil {
		return nil, err
	}

	db := url.URL{Scheme: "postgres", User: url.User("photon"), Path: "/photon", RawQuery: url.Values{"host": {s.home}}.Encode()}
	kv := url.URL{Scheme: "valkey", User: url.UserPassword("", s.password), Host: valkeyAddress()}
	env := os.Environ()
	// pg_dump, pg_restore and psql back the database up and put it back.
	env = append(env, "PATH="+s.postgres()+";"+os.Getenv("PATH"), "PHOTON_DATABASE_URL="+db.String(), "PHOTON_VALKEY_URL="+kv.String())
	return env, nil
}

func (s *server) initdb() error {
	if err := s.tool("initdb", "-D", s.data(), "-U", "photon", "--auth=trust", "-E", "UTF8", "--locale=C"); err != nil {
		return err
	}
	conf, err := os.OpenFile(filepath.Join(s.data(), "postgresql.conf"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(conf, "listen_addresses = ''\nunix_socket_directories = '%s'\n", filepath.ToSlash(s.home))
	return errors.Join(err, conf.Close())
}

func (s *server) startValkey() error {
	s.password = rand.Text()
	log, err := s.logFile("valkey.log")
	if err != nil {
		return err
	}
	defer log.Close()
	// As the compose file's: nothing durable lives here, so snapshots only keep live sessions
	// across a restart. The password comes on stdin, which no other user can read, as "-" asks.
	p := exec.CommandContext(context.Background(), filepath.Join(s.app, "valkey", "bin", "valkey-server.exe"), "-", //nolint:gosec // the install's own Valkey
		"--bind", "127.0.0.1", "--port", strconv.Itoa(valkeyPort), "--dir", filepath.ToSlash(s.home),
		"--appendonly", "no", "--save", "300 1", "--maxmemory", "256mb", "--maxmemory-policy", "volatile-lru")
	p.Stdin = strings.NewReader("requirepass " + s.password + "\n")
	p.Stdout, p.Stderr = log, log
	p.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := p.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() {
		err := p.Wait()
		s.logger.Info("valkey-server exited", slog.Any("err", err))
		close(exited)
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for range 100 {
		if c, err := dialValkey(); err == nil {
			s.mu.Lock()
			s.valkey, s.vexited = p, exited
			s.mu.Unlock()
			return c.Close()
		}
		<-tick.C
	}
	return errors.Join(errors.New("valkey did not start: see valkey.log"), p.Process.Kill())
}

// toolsLog is where PostgreSQL's tools write; pg_ctl writes the server's own log to postgres.log,
// which Windows lets no other process hold open for writing.
const toolsLog = "postgres-tools.log"

// tool runs one of PostgreSQL's tools to its end, all it says written to toolsLog. Its output is
// a file, never a pipe: the server pg_ctl starts holds what pg_ctl was given, and a pipe would
// stay open while it runs.
func (s *server) tool(name string, args ...string) error {
	log, err := s.logFile(toolsLog)
	if err != nil {
		return err
	}
	defer log.Close()
	return s.runTool(log, log, name, args...)
}

// query answers what psql prints for sql, run against the postgres database.
func (s *server) query(sql string) (string, error) {
	log, err := s.logFile(toolsLog)
	if err != nil {
		return "", err
	}
	defer log.Close()
	var out strings.Builder
	err = s.runTool(&out, log, "psql", "-h", s.home, "-U", "photon", "-d", "postgres", "-Atc", sql)
	return strings.TrimSpace(out.String()), err
}

func (s *server) runTool(stdout, stderr io.Writer, name string, args ...string) error {
	p := exec.CommandContext(context.Background(), filepath.Join(s.postgres(), name+".exe"), args...) //nolint:gosec // the install's own PostgreSQL
	p.Stdout, p.Stderr = stdout, stderr
	p.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := p.Run(); err != nil {
		return fmt.Errorf("%s failed: see %s", name, toolsLog)
	}
	return nil
}

func (s *server) logFile(name string) (io.WriteCloser, error) {
	if err := os.MkdirAll(s.logs(), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(s.logs(), name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
