//go:build windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "pkg-helper is only supported on Unix-like systems")
	os.Exit(1)
}
