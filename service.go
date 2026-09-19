package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const serviceMarker = "Managed by Apptrail service install."
const launchLabel = "io.github.samishal1998.apptrail"

type serviceConfig struct{ Executable, Addr, Data, Origin string }

func serviceCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: apptrail service install [-addr ADDRESS] [-data DIRECTORY] [-origin URL]\n       apptrail service <start|stop|restart|status|uninstall>\n\nInstall enables and starts a systemd user service (Linux), LaunchAgent (macOS),\nor automatic Windows Service (Administrator required). Uninstall preserves app data.")
		return nil
	}
	action := args[0]
	switch action {
	case "install", "start", "stop", "restart", "status", "uninstall":
	default:
		return fmt.Errorf("unknown service action %q; use apptrail service --help", action)
	}
	var cfg serviceConfig
	if action == "install" {
		defaultData, err := serviceDataDir()
		if err != nil {
			return err
		}
		flags := flag.NewFlagSet("apptrail service install", flag.ContinueOnError)
		flags.StringVar(&cfg.Addr, "addr", "0.0.0.0:8080", "Listen address")
		flags.StringVar(&cfg.Data, "data", defaultData, "Persistent data directory; use your existing directory to preserve your account")
		flags.StringVar(&cfg.Origin, "origin", os.Getenv("APPTRAIL_ORIGIN"), "Browser-facing origin for HTTPS reverse proxies")
		if err = flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
			return nil
		} else if err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected service arguments: %v", flags.Args())
		}
		cfg.Executable, err = os.Executable()
		if err != nil {
			return err
		}
		cfg.Data, err = filepath.Abs(cfg.Data)
		if err != nil {
			return err
		}
		if err = validateServiceConfig(cfg); err != nil {
			return err
		}
	} else if len(args) != 1 {
		return fmt.Errorf("%s does not accept flags; set configuration with service install", action)
	}
	if err := manageService(action, cfg); err != nil {
		return err
	}
	if action == "install" {
		fmt.Printf("Apptrail service installed and started.\nData: %s\nSetup token: %s\n", cfg.Data, filepath.Join(cfg.Data, "setup-token"))
	}
	return nil
}

func serviceDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		root := os.Getenv("ProgramData")
		if root == "" {
			root = `C:\ProgramData`
		}
		return filepath.Join(root, "Apptrail"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Apptrail"), nil
	}
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		root = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("XDG_DATA_HOME must be absolute")
	}
	return filepath.Join(root, "apptrail"), nil
}
func validateOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	u, err := validURL(origin)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("origin must be an HTTP(S) origin, such as https://apptrail.example.com")
	}
	return nil
}
func validateServiceConfig(c serviceConfig) error {
	for _, value := range []string{c.Executable, c.Data, c.Addr, c.Origin} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("service configuration cannot contain NUL or newlines")
		}
	}
	if !filepath.IsAbs(c.Executable) || !filepath.IsAbs(c.Data) {
		return fmt.Errorf("service executable and data paths must be absolute")
	}
	_, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("service port must be between 1 and 65535")
	}
	return validateOrigin(c.Origin)
}
func (c serviceConfig) arguments() []string {
	return []string{"-addr", c.Addr, "-data", c.Data, "-origin", c.Origin}
}
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	if len(out) > 0 {
		fmt.Print(string(out))
	}
	return nil
}
func managedFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to modify non-regular service file %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(serviceMarker)) {
		return fmt.Errorf("%s already exists and is not managed by Apptrail; back it up and remove it before installing", path)
	}
	return nil
}
func writeServiceFile(path, content string) error {
	if err := managedFile(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".apptrail-service-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func systemdUnit(c serviceConfig) string {
	args := append([]string{c.Executable}, c.arguments()...)
	for i, arg := range args {
		args[i] = strconv.Quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(arg))
	}
	return "# " + serviceMarker + "\n[Unit]\nDescription=Apptrail self-hosted dashboard\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=" + strings.Join(args, " ") + "\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=45\nUMask=0077\n\n[Install]\nWantedBy=default.target\n"
}
func xmlString(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return "<string>" + b.String() + "</string>"
}
func launchAgent(c serviceConfig) string {
	args := append([]string{c.Executable}, c.arguments()...)
	var arguments strings.Builder
	for _, arg := range args {
		arguments.WriteString(xmlString(arg))
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!-- ` + serviceMarker + ` -->
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key>` + xmlString(launchLabel) + `
<key>ProgramArguments</key><array>` + arguments.String() + `</array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ProcessType</key><string>Background</string>
<key>ExitTimeOut</key><integer>45</integer>
<key>StandardOutPath</key>` + xmlString(filepath.Join(c.Data, "apptrail.log")) + `
<key>StandardErrorPath</key>` + xmlString(filepath.Join(c.Data, "apptrail.log")) + `
</dict></plist>
`
}
