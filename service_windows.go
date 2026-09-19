package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const windowsServiceName = "Apptrail"

func runManagedServer(addr, data, origin string) error {
	managed, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !managed {
		return runConsoleServer(addr, data, origin)
	}
	if err = os.MkdirAll(data, 0700); err != nil {
		return err
	}
	// ponytail: append-only service log; add rotation if high-volume installations need bounded log storage.
	f, err := os.OpenFile(filepath.Join(data, "apptrail.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	log.SetOutput(f)
	return svc.Run(windowsServiceName, &windowsHandler{addr, data, origin})
}

type windowsHandler struct{ addr, data, origin string }

func (h *windowsHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status <- svc.Status{State: svc.StartPending, WaitHint: 30000}
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, h.addr, h.data, h.origin) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case err := <-done:
			if err != nil {
				log.Print(err)
				return true, 1
			}
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 45000}
				cancel()
				if err := <-done; err != nil {
					log.Print(err)
					return true, 1
				}
				return false, 0
			}
		}
	}
}
func waitWindowsService(s *mgr.Service, wanted svc.State) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		state, err := s.Query()
		if err != nil {
			return err
		}
		if state.State == wanted {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach state %d; inspect Services and the data directory's apptrail.log", wanted)
}
func stopWindowsService(s *mgr.Service) error {
	state, err := s.Query()
	if err != nil {
		return err
	}
	if state.State == svc.Stopped {
		return nil
	}
	if _, err = s.Control(svc.Stop); err != nil {
		return err
	}
	return waitWindowsService(s, svc.Stopped)
}
func manageService(action string, c serviceConfig) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("open Windows Service Manager from an Administrator terminal: %w", err)
	}
	defer m.Disconnect()
	if action == "install" {
		if existing, e := m.OpenService(windowsServiceName); e == nil {
			existing.Close()
			return fmt.Errorf("Apptrail service already exists; uninstall it before reinstalling (data is preserved)")
		}
		root := os.Getenv("ProgramFiles")
		if root == "" {
			return fmt.Errorf("ProgramFiles is not set")
		}
		dir := filepath.Join(root, "Apptrail")
		binary := filepath.Join(dir, "apptrail.exe")
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err = os.MkdirAll(c.Data, 0700); err != nil {
			return err
		}
		if !strings.EqualFold(filepath.Clean(c.Executable), filepath.Clean(binary)) {
			in, err := os.Open(c.Executable)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.CreateTemp(dir, ".apptrail-*.exe")
			if err != nil {
				return err
			}
			defer os.Remove(out.Name())
			_, copyErr := io.Copy(out, in)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err = os.Rename(out.Name(), binary); err != nil {
				return fmt.Errorf("install executable; stop any running Apptrail process first: %w", err)
			}
		}
		if err = runCommand("icacls", binary, "/grant", "*S-1-5-19:RX"); err != nil {
			return err
		}
		if err = runCommand("icacls", c.Data, "/grant", "*S-1-5-19:(OI)(CI)M", "/T"); err != nil {
			return err
		}
		s, err := m.CreateService(windowsServiceName, binary, mgr.Config{DisplayName: "Apptrail", Description: "Apptrail self-hosted dashboard", StartType: mgr.StartAutomatic, ServiceStartName: `NT AUTHORITY\LocalService`}, c.arguments()...)
		if err != nil {
			return err
		}
		defer s.Close()
		if err = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}, 60); err != nil {
			return err
		}
		if err = s.Start(); err != nil {
			return err
		}
		if err = waitWindowsService(s, svc.Running); err != nil {
			return err
		}
		fmt.Printf("Windows Service: %s (LocalService, automatic startup)\nExecutable: %s\nLogs: %s\n", windowsServiceName, binary, filepath.Join(c.Data, "apptrail.log"))
		return nil
	}
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	switch action {
	case "status":
		state, err := s.Query()
		if err != nil {
			return err
		}
		names := map[svc.State]string{svc.Running: "running", svc.Stopped: "stopped", svc.StartPending: "starting", svc.StopPending: "stopping"}
		fmt.Printf("Apptrail: %s (state %d)\n", names[state.State], state.State)
		return nil
	case "stop":
		return stopWindowsService(s)
	case "uninstall":
		if err = stopWindowsService(s); err != nil {
			return err
		}
		return s.Delete()
	case "restart":
		if err = stopWindowsService(s); err != nil {
			return err
		}
	case "start":
		state, err := s.Query()
		if err != nil {
			return err
		}
		if state.State == svc.Running {
			return nil
		}
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	if err = s.Start(); err != nil {
		return err
	}
	return waitWindowsService(s, svc.Running)
}
