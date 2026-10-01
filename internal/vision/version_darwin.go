package vision

import "syscall"

// macOSVersion is the running macOS's product version, such as "14.5", or
// "" if the kernel does not say.
func macOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return v
}
