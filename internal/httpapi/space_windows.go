package httpapi

import "golang.org/x/sys/windows"

// freeBytes is the space left to the server under path.
func freeBytes(path string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var free uint64
	if windows.GetDiskFreeSpaceEx(p, &free, nil, nil) != nil {
		return 0, false
	}
	return free, true
}
