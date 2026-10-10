package media

import "os"

// stop kills a tool: Windows has no signal to ask a process to finish what it is writing.
func stop(p *os.Process) error { return p.Kill() }
