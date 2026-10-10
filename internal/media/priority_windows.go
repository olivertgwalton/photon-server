package media

import (
	"errors"

	"golang.org/x/sys/windows"
)

// lower gives a process BelowNormal priority, as Jellyfin does its. A process already gone is not
// lowered, and that is not an error.
func lower(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(windows.SetPriorityClass(h, windows.BELOW_NORMAL_PRIORITY_CLASS), windows.CloseHandle(h))
}
