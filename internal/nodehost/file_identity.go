package nodehost

import (
	"fmt"
	"os"
	"syscall"
)

// FileIdentityStat holds device and inode numbers for file identity comparison.
type FileIdentityStat struct {
	Dev uint64
	Ino uint64
}

// GetFileIdentity returns the device and inode numbers for a path.
func GetFileIdentity(path string) (FileIdentityStat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileIdentityStat{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileIdentityStat{}, fmt.Errorf("unsupported platform: cannot extract file identity for %s", path)
	}
	return FileIdentityStat{
		Dev: uint64(stat.Dev),
		Ino: stat.Ino,
	}, nil
}

// SameFileIdentity compares two file identity stats for equality.
func SameFileIdentity(left, right FileIdentityStat) bool {
	return left.Ino == right.Ino && left.Dev == right.Dev
}
