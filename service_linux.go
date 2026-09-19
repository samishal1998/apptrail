package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runManagedServer(addr, data, origin string) error { return runConsoleServer(addr, data, origin) }
func manageService(action string, c serviceConfig) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("service management requires systemd; run Apptrail in the foreground on other Linux systems")
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(root, "systemd", "user", "apptrail.service")
	ctl := func(args ...string) error { return runCommand("systemctl", append([]string{"--user"}, args...)...) }
	switch action {
	case "install":
		if os.Geteuid() == 0 {
			return fmt.Errorf("install the user service as your normal user, without sudo")
		}
		if err = os.MkdirAll(c.Data, 0700); err != nil {
			return err
		}
		if err = writeServiceFile(path, systemdUnit(c)); err != nil {
			return err
		}
		if err = ctl("daemon-reload"); err != nil {
			return err
		}
		if err = ctl("enable", "apptrail.service"); err != nil {
			return err
		}
		if err = ctl("restart", "apptrail.service"); err != nil {
			return err
		}
		fmt.Printf("Unit: %s\nLogs: journalctl --user -u apptrail -f\nFor startup before login, enable lingering: loginctl enable-linger %q\n", path, os.Getenv("USER"))
		return nil
	case "uninstall":
		if err = managedFile(path); err != nil {
			return err
		}
		if err = ctl("disable", "--now", "apptrail.service"); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
		return ctl("daemon-reload")
	case "status":
		return ctl("status", "apptrail.service", "--no-pager")
	default:
		if err = managedFile(path); err != nil {
			return err
		}
		return ctl(action, "apptrail.service")
	}
}
