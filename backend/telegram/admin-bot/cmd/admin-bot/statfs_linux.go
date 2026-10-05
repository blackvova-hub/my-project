//go:build linux

package main

import (
	"fmt"
	"syscall"
)

func statfs(path, label string) string {
	var data syscall.Statfs_t
	if err := syscall.Statfs(path, &data); err != nil || data.Blocks == 0 {
		return label + ": unavailable"
	}
	total := data.Blocks * uint64(data.Bsize)
	available := data.Bavail * uint64(data.Bsize)
	return fmt.Sprintf("%s: used=%s, free=%s, total=%s", label, humanBytes(int64(total-available)), humanBytes(int64(available)), humanBytes(int64(total)))
}
