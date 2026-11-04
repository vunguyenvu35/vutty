//go:build !linux && !darwin

package localtty

// processCwd is not available here: the terminal's folder is only known when the shell reports it.
func processCwd(int) string { return "" }
