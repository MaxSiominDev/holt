//go:build unix

package textblock

import (
	"io/fs"
	"syscall"
)

// names reports how many directory entries point at the file behind info.
func names(info fs.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 1
	}
	return uint64(stat.Nlink) //nolint:unconvert
}
