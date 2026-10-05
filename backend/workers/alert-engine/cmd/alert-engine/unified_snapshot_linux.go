//go:build linux

package main

import "os"

func replaceSnapshotFile(source, destination string) error {
	return os.Rename(source, destination)
}

func syncSnapshotDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
