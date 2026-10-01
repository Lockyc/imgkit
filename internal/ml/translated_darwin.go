package ml

import "syscall"

// translated reads sysctl.proc_translated: "1" when this process runs under
// Rosetta, "0" when native. The kernel returns a little-endian int32; a Mac
// too old to have the sysctl reads as "".
func translated() string {
	v, err := syscall.Sysctl("sysctl.proc_translated")
	if err != nil || v == "" {
		return ""
	}
	if v[0] == 1 {
		return "1"
	}
	return "0"
}
