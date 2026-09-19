package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runManagedServer(addr, data, origin string) error { return runConsoleServer(addr, data, origin) }
func manageService(action string, c serviceConfig) error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("install the LaunchAgent as your normal user, without sudo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	target := domain + "/" + launchLabel
	loaded := func() bool { return exec.Command("launchctl", "print", target).Run() == nil }
	stop := func() error {
		if !loaded() {
			return nil
		}
		return runCommand("launchctl", "bootout", target)
	}
	start := func() error {
		if err := runCommand("launchctl", "enable", target); err != nil {
			return err
		}
		if loaded() {
			return runCommand("launchctl", "kickstart", target)
		}
		return runCommand("launchctl", "bootstrap", domain, path)
	}
	if action == "status" {
		return runCommand("launchctl", "print", target)
	}
	if action != "install" {
		if err = managedFile(path); err != nil {
			return err
		}
	}
	switch action {
	case "install":
		if err = os.MkdirAll(c.Data, 0700); err != nil {
			return err
		}
		if err = writeServiceFile(path, launchAgent(c)); err != nil {
			return err
		}
		if err = stop(); err != nil {
			return err
		}
		if err = start(); err != nil {
			return fmt.Errorf("LaunchAgent could not start; install from a logged-in macOS desktop session: %w", err)
		}
		fmt.Printf("LaunchAgent: %s\nLogs: %s\nStarts automatically when you log in.\n", path, filepath.Join(c.Data, "apptrail.log"))
		return nil
	case "start":
		return start()
	case "stop":
		return stop()
	case "restart":
		if err = stop(); err != nil {
			return err
		}
		return start()
	case "uninstall":
		if err = stop(); err != nil {
			return err
		}
		return os.Remove(path)
	}
	return fmt.Errorf("unknown service action %q", action)
}
