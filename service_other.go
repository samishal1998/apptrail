//go:build !linux && !darwin && !windows

package main

import "fmt"

func runManagedServer(addr, data, origin string) error { return runConsoleServer(addr, data, origin) }
func manageService(string, serviceConfig) error {
	return fmt.Errorf("native service management is supported on Linux, macOS, and Windows")
}
