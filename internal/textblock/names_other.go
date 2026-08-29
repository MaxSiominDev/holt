//go:build !unix

package textblock

import "io/fs"

// names cannot count them here, so writeWhole keeps its atomic rename. Only an
// operating system's name carries a build constraint, which is why these files exist.
func names(fs.FileInfo) uint64 {
	return 1
}
