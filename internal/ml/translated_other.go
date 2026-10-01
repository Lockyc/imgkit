//go:build !darwin

package ml

// translated is "" off macOS: there is no Rosetta.
func translated() string { return "" }
