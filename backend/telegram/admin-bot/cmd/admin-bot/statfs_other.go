//go:build !linux

package main

func statfs(_ string, label string) string {
	return label + ": unavailable on this development OS"
}
