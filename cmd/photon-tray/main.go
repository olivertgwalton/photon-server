//go:build windows

// Command photon-tray is Photon for Windows: an icon in the notification area that runs
// photon-server beside the PostgreSQL and Valkey it carries, as Photon.app does on the Mac. It is
// built as Photon.exe and installed per user, so it sees the user's drives and needs no admin.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/olivertgwalton/photon-server/internal/release"
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	shell32               = windows.NewLazySystemDLL("shell32.dll")
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	registerClassEx       = user32.NewProc("RegisterClassExW")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	postMessage           = user32.NewProc("PostMessageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	destroyMenu           = user32.NewProc("DestroyMenu")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	getCursorPos          = user32.NewProc("GetCursorPos")
	getSystemMetrics      = user32.NewProc("GetSystemMetrics")
	loadImage             = user32.NewProc("LoadImageW")
	shellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	attachConsole         = kernel32.NewProc("AttachConsole")
	freeConsole           = kernel32.NewProc("FreeConsole")
)

const (
	wmNull              = 0x0000
	wmDestroy           = 0x0002
	wmClose             = 0x0010
	wmEndSession        = 0x0016
	wmLButtonUp         = 0x0202
	wmRButtonUp         = 0x0205
	wmApp               = 0x8000
	wmTray              = wmApp + 1 // the icon's own messages
	wmState             = wmApp + 2 // the server's state changed
	wmNotice            = wmApp + 3 // there is a notice to show
	wmInstall           = wmApp + 4 // an update's installer is downloaded
	ninBalloonUserClick = 0x0405
	nimAdd              = 0
	nimModify           = 1
	nimDelete           = 2
	nimSetVersion       = 4
	nifMessage          = 0x1
	nifIcon             = 0x2
	nifTip              = 0x4
	nifInfo             = 0x10
	niifInfo            = 0x1
	// notifyIconVersion is the version whose callbacks say a balloon was clicked and keep the
	// mouse's message in lParam.
	notifyIconVersion = 3
	mfGrayed          = 0x1
	mfChecked         = 0x8
	mfSeparator       = 0x800
	tpmRightButton    = 0x2
	tpmReturnCmd      = 0x100
	imageIcon         = 1
	lrLoadFromFile    = 0x10
	smCxSmIcon        = 49
	smCySmIcon        = 50
	swShowNormal      = 1
)

// className names the window the installer finds to ask a running Photon to quit.
const className = "Photon"

// The menu's commands.
const (
	cmdOpen = iota + 1
	cmdAtSignIn
	cmdLogs
	cmdUpdate
	cmdQuit
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type point struct{ x, y int32 }

type notifyIconData struct {
	size            uint32
	hwnd            windows.HWND
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            windows.Handle
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        windows.GUID
	balloonIcon     windows.Handle
}

type tray struct {
	server         *server
	logger         *slog.Logger
	exe            string
	hwnd           windows.HWND
	icon           notifyIconData
	taskbarCreated uint32
	// ctx is cancelled as the tray goes, which stops its looking for and downloading updates.
	ctx    context.Context
	cancel context.CancelFunc
	client *http.Client
	// offered is whether the balloon showing offers an update, which a click on it installs.
	offered bool

	// mu guards what the goroutines that look for and download updates leave for the window's
	// thread.
	mu          sync.Mutex
	offer       *update
	downloading bool
	installer   string
	notice      *notice
}

// notice is a balloon to show from the icon.
type notice struct {
	title, text string
	offer       bool
}

func main() {
	os.Exit(run())
}

func run() int {
	runtime.LockOSThread()
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	// A second Photon opens the first's web app instead.
	mutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(`Local\io.github.olivertgwalton.photon`))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if err := open(address); err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		return 1
	}
	defer windows.CloseHandle(mutex) //nolint:errcheck // Photon is exiting, and Windows lets it go anyway
	// os.UserCacheDir is %LOCALAPPDATA%, which is not roamed with the profile, as a database
	// must not be.
	local, err := os.UserCacheDir()
	if err != nil {
		return 1
	}
	home := filepath.Join(local, "Photon")
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0o700); err != nil {
		return 1
	}
	log, err := os.OpenFile(filepath.Join(home, "logs", "photon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	defer log.Close()
	logger := slog.New(slog.NewJSONHandler(log, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t := &tray{exe: exe, logger: logger, ctx: ctx, cancel: cancel, client: &http.Client{}}
	t.server = newServer(filepath.Dir(exe), home, logger, func() { postMessage.Call(uintptr(t.hwnd), wmState, 0, 0) })
	if err := t.create(); err != nil {
		logger.Error("photon not started", slog.Any("err", err))
		return 1
	}
	t.server.start()
	if release.Is(version) {
		go t.lookEvery(ctx)
	}
	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return 0
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// create makes the tray's window, hidden but top level, so that it is told when Windows ends the
// session and asked to close by taskkill and the installer, and adds its icon.
func (t *tray) create() error {
	class := windows.StringToUTF16Ptr(className)
	wc := wndClassEx{wndProc: windows.NewCallback(t.wndProc), className: class}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	hwnd, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return err
	}
	t.hwnd = windows.HWND(hwnd)
	t.taskbarCreated = registerMessage("TaskbarCreated")
	cx, _, _ := getSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := getSystemMetrics.Call(smCySmIcon)
	icon := windows.StringToUTF16Ptr(filepath.Join(filepath.Dir(t.exe), "Photon.ico"))
	h, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(icon)), imageIcon, cx, cy, lrLoadFromFile)
	t.icon = notifyIconData{hwnd: t.hwnd, id: 1, flags: nifMessage | nifIcon | nifTip, callbackMessage: wmTray, icon: windows.Handle(h)}
	t.icon.size = uint32(unsafe.Sizeof(t.icon))
	t.setTip()
	t.add()
	return nil
}

func (t *tray) add() {
	t.notify(nimAdd)
	t.icon.version = notifyIconVersion
	t.notify(nimSetVersion)
}

func registerMessage(name string) uint32 {
	r, _, _ := registerWindowMessage.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))))
	return uint32(r)
}

func (t *tray) notify(action uintptr) {
	shellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&t.icon)))
}

func (t *tray) setTip() {
	tip, err := windows.UTF16FromString("Photon: " + t.server.current().String())
	if err != nil {
		return
	}
	t.icon.tip = [128]uint16{}
	copy(t.icon.tip[:len(t.icon.tip)-1], tip)
}

func (t *tray) wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch {
	case message == wmTray && (lParam == wmLButtonUp || lParam == wmRButtonUp):
		t.menu()
	case message == wmTray && lParam == ninBalloonUserClick && t.offered:
		t.install()
	case message == wmNotice:
		t.showNotice()
	case message == wmInstall:
		t.runInstaller()
	case message == wmState:
		t.setTip()
		t.notify(nimModify)
	case t.taskbarCreated != 0 && message == uintptr(t.taskbarCreated):
		// Explorer started again, with a notification area that has no icon in it.
		t.add()
	case message == wmClose:
		t.quit()
		destroyWindow.Call(hwnd)
	case message == wmEndSession && wParam != 0:
		t.quit()
	case message == wmDestroy:
		postQuitMessage.Call(0)
	default:
		r, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
		return r
	}
	return 0
}

// quit stops the server and its services before the tray goes, however it is closed.
func (t *tray) quit() {
	t.cancel()
	t.server.stop()
	t.notify(nimDelete)
}

func (t *tray) menu() {
	m, _, _ := createPopupMenu.Call()
	defer destroyMenu.Call(m)
	st := t.server.current()
	item(m, mfGrayed, 0, st.String())
	item(m, mfSeparator, 0, "")
	item(m, flagIf(st.kind != running, mfGrayed), cmdOpen, "Open Photon")
	item(m, flagIf(t.atSignIn(), mfChecked), cmdAtSignIn, "Start at Sign-in")
	item(m, 0, cmdLogs, "Show Logs")
	item(m, mfSeparator, 0, "")
	t.mu.Lock()
	switch {
	case !release.Is(version):
		item(m, mfGrayed, cmdUpdate, "Check for Updates…")
	case t.downloading:
		item(m, mfGrayed, 0, "Downloading Photon "+t.offer.Version+"…")
	case t.offer != nil:
		item(m, 0, cmdUpdate, "Install Photon "+t.offer.Version+"…")
	default:
		item(m, 0, cmdUpdate, "Check for Updates…")
	}
	offer := t.offer != nil
	t.mu.Unlock()
	item(m, mfSeparator, 0, "")
	item(m, 0, cmdQuit, "Quit Photon")
	var p point
	getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// The menu closes when clicked away from only if the window is in front.
	setForegroundWindow.Call(uintptr(t.hwnd))
	cmd, _, _ := trackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton, uintptr(p.x), uintptr(p.y), 0, uintptr(t.hwnd), 0)
	postMessage.Call(uintptr(t.hwnd), wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		t.report("open", open(address))
	case cmdAtSignIn:
		t.report("start at sign-in", t.setAtSignIn(!t.atSignIn()))
	case cmdLogs:
		t.report("show logs", windows.ShellExecute(0, nil, windows.StringToUTF16Ptr("explorer.exe"),
			windows.StringToUTF16Ptr(`/select,"`+filepath.Join(t.server.logs(), "photon-server.log")+`"`), nil, swShowNormal))
	case cmdUpdate:
		if offer {
			t.install()
		} else {
			go t.look(t.ctx, true)
		}
	case cmdQuit:
		postMessage.Call(uintptr(t.hwnd), wmClose, 0, 0)
	}
}

// lookEvery looks for an update now and every checkEvery until ctx ends, as Sparkle does.
func (t *tray) lookEvery(ctx context.Context) {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		t.look(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// look looks for an update and offers one it finds; asked, it also says when there is none.
func (t *tray) look(ctx context.Context, asked bool) {
	u, newer, err := check(ctx, t.client, version)
	var n *notice
	switch {
	case err != nil:
		t.logger.WarnContext(ctx, "no update looked for", slog.Any("err", err))
		if asked {
			n = &notice{title: "Photon could not look for updates", text: err.Error()}
		}
	case newer:
		n = &notice{title: "Photon " + u.Version + " is available", text: "Click here to install it.", offer: true}
	case asked:
		n = &notice{title: "Photon is up to date", text: "Photon " + version + " is the latest release."}
	}
	if newer {
		t.mu.Lock()
		t.offer = &u
		t.mu.Unlock()
	}
	if n == nil {
		return
	}
	t.mu.Lock()
	t.notice = n
	t.mu.Unlock()
	postMessage.Call(uintptr(t.hwnd), wmNotice, 0, 0)
}

func (t *tray) showNotice() {
	t.mu.Lock()
	n := t.notice
	t.notice = nil
	t.mu.Unlock()
	if n == nil {
		return
	}
	title, err := windows.UTF16FromString(n.title)
	if err != nil {
		return
	}
	text, err := windows.UTF16FromString(n.text)
	if err != nil {
		return
	}
	t.icon.infoTitle, t.icon.info = [64]uint16{}, [256]uint16{}
	copy(t.icon.infoTitle[:len(t.icon.infoTitle)-1], title)
	copy(t.icon.info[:len(t.icon.info)-1], text)
	t.icon.flags |= nifInfo
	t.icon.infoFlags = niifInfo
	t.notify(nimModify)
	t.icon.flags &^= nifInfo
	t.offered = n.offer
}

// install downloads the update on offer, which an admin clicked to install, and installs it once
// it is the one its signed manifest names.
func (t *tray) install() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.offer == nil || t.downloading {
		return
	}
	t.downloading = true
	u := *t.offer
	go func() {
		name, err := download(t.ctx, t.client, u, filepath.Join(t.server.home, "updates"))
		t.mu.Lock()
		t.downloading = false
		t.installer = name
		if err != nil {
			t.logger.Error("update not downloaded", slog.String("version", u.Version), slog.Any("err", err))
			t.notice = &notice{title: "Photon was not updated", text: err.Error()}
		}
		t.mu.Unlock()
		if err != nil {
			postMessage.Call(uintptr(t.hwnd), wmNotice, 0, 0)
			return
		}
		postMessage.Call(uintptr(t.hwnd), wmInstall, 0, 0)
	}()
}

// runInstaller stops the server and its services, starts the installer and goes: the installer
// waits for the tray to be gone, replaces it, and starts the new one, which migrates the database.
func (t *tray) runInstaller() {
	t.mu.Lock()
	name := t.installer
	t.mu.Unlock()
	t.server.stop()
	p := exec.CommandContext(context.Background(), name, "/S")
	if err := p.Start(); err != nil {
		t.logger.Error("installer not started", slog.Any("err", err))
		t.mu.Lock()
		t.notice = &notice{title: "Photon was not updated", text: err.Error()}
		t.mu.Unlock()
		t.showNotice()
		t.server.start()
		return
	}
	t.logger.Info("installer started", slog.String("installer", name))
	t.cancel()
	t.notify(nimDelete)
	destroyWindow.Call(uintptr(t.hwnd))
}

func item(m, flags, id uintptr, text string) {
	appendMenu.Call(m, flags, id, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))))
}

func flagIf(on bool, flag uintptr) uintptr {
	if on {
		return flag
	}
	return 0
}

func (t *tray) report(command string, err error) {
	if err != nil {
		t.logger.Error("menu command failed", slog.String("command", command), slog.Any("err", err))
	}
}

func open(url string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(url), nil, nil, swShowNormal)
}

// Starting at sign-in is the user's Run entry, which the uninstaller takes away.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func (t *tray) atSignIn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(className)
	return err == nil
}

func (t *tray) setAtSignIn(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue(className, `"`+t.exe+`"`)
	}
	return k.DeleteValue(className)
}
