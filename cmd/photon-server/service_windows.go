package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
)

// serviceName is the name the service is created under: sc.exe create photon-server.
const serviceName = "photon-server"

// asService runs the server under Windows' service manager when it is started by one, answering
// whether it was and the code it exits with. A service has no console, so it logs to
// photon-server.log in its cache folder.
func asService(args []string) (bool, int) {
	is, err := svc.IsWindowsService()
	if err != nil || !is {
		return false, 0
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return true, 1
	}
	dir = filepath.Join(dir, "photon-server")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return true, 1
	}
	// ponytail: the log is appended to and never rotated; rotate it if a year's grows too big.
	log, err := os.OpenFile(filepath.Join(dir, "photon-server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return true, 1
	}
	defer log.Close()
	s := &service{logger: slog.New(slog.NewJSONHandler(log, nil)), args: args}
	if err := svc.Run(serviceName, s); err != nil {
		s.logger.Error("photon-server not run as a service", slog.Any("err", err))
		return true, 1
	}
	return true, s.code
}

type service struct {
	logger *slog.Logger
	args   []string
	code   int
}

// Execute runs the server until it ends, a stop asking it to drain its streams as SIGTERM does,
// and a shutdown, which waits for nothing, to stop at once.
func (s *service) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	stops := make(chan os.Signal, 2)
	done := make(chan int, 1)
	go func() { done <- exitCode(s.logger, run(s.logger, s.args, stops)) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case s.code = <-done:
			// A code of its own fails the service, so its recovery actions start it again.
			return s.code != 0, uint32(s.code)
		case r := <-requests:
			switch r.Cmd { //nolint:exhaustive // it accepts stop and shutdown alone, so the manager asks nothing else
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop:
				status <- svc.Status{State: svc.StopPending, WaitHint: uint32(drainFor / time.Millisecond)}
				stops <- syscall.SIGTERM
			case svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				stops <- syscall.SIGTERM
				stops <- syscall.SIGTERM
			}
		}
	}
}
