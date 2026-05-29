package tool

import "path/filepath"

// resolve returns an absolute path for p, interpreting relative paths against the
// tool context's working directory.
func resolve(tctx Context, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	base := tctx.Directory
	if base == "" {
		base = "."
	}
	return filepath.Join(base, p)
}
