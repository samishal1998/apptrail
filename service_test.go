package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceConfigurationAndOwnership(t *testing.T) {
	dir := t.TempDir()
	c := serviceConfig{Executable: filepath.Join(dir, "app trail"), Data: filepath.Join(dir, `data % $ & " space`), Addr: "127.0.0.1:19173", Origin: "https://apptrail.example.com"}
	if err := validateServiceConfig(c); err != nil {
		t.Fatal(err)
	}
	unit := systemdUnit(c)
	if !strings.Contains(unit, "%% $$ & \\\"") || !strings.Contains(unit, "TimeoutStopSec=45") {
		t.Fatalf("systemd literals are not escaped: %s", unit)
	}
	decoder := xml.NewDecoder(strings.NewReader(launchAgent(c)))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
			var value string
			if err = decoder.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	if !contains(values, c.Data) || !contains(values, c.Executable) || !contains(values, c.Origin) {
		t.Fatalf("launchd arguments changed through XML encoding: %v", values)
	}
	path := filepath.Join(dir, "service.conf")
	if err := os.WriteFile(path, []byte("User-owned service"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeServiceFile(path, unit); err == nil {
		t.Fatal("overwrote an unmanaged service")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := writeServiceFile(path, unit); err != nil {
		t.Fatal(err)
	}
	c.Addr = "127.0.0.1:19174"
	if err := writeServiceFile(path, systemdUnit(c)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []serviceConfig{
		{Executable: c.Executable, Data: c.Data + "\nInjected", Addr: c.Addr},
		{Executable: c.Executable, Data: c.Data, Addr: "127.0.0.1:0"},
		{Executable: c.Executable, Data: c.Data, Addr: c.Addr, Origin: "https://example.com/path"},
	} {
		if err := validateServiceConfig(bad); err == nil {
			t.Fatalf("accepted invalid service configuration: %+v", bad)
		}
	}
}
