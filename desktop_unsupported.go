//go:build !darwin && !linux

package main

import "errors"

func runDesktop(opts cliOptions) error {
	return errors.New("desktop mode is supported only on macOS and Linux")
}
