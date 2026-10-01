//go:build !darwin

package vision

// macOSVersion is "" off macOS.
func macOSVersion() string { return "" }
