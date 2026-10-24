package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStewardExportRetainsNativeIdentityAndRejectsPartialStaleInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	snapshot := map[string]any{"schema_version": "1", "kind": "steward.inventory", "host_id": "0123456789abcdef", "revision": "revision", "observed_at": time.Now().Unix(), "complete": true, "sources": []any{map[string]any{"id": "edge", "kind": "proxy", "complete": true}}, "resources": []any{map[string]any{"kind": "route", "id": "app", "binding": "edge", "scope": "user", "name": "App", "enabled": true, "ownership": "owned_definition", "observed": "present", "public_url": "https://app.example.test"}}}
	write := func() {
		t.Helper()
		b, e := json.Marshal(snapshot)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	urlPath := filepath.ToSlash(path)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	p := provider{Type: "steward", Endpoint: (&url.URL{Scheme: "file", Path: urlPath}).String()}
	write()
	observations, _, err := stewardSnapshot(context.Background(), p)
	if err != nil || len(observations) != 1 {
		t.Fatalf("%v %#v", err, observations)
	}
	if observations[0].Source != "steward:0123456789abcdef:user:route:edge:app" || observations[0].Facts["url"].Value != "https://app.example.test" {
		t.Fatal(observations)
	}
	snapshot["complete"] = false
	write()
	if _, _, err = stewardSnapshot(context.Background(), p); err == nil {
		t.Fatal("partial accepted")
	}
	snapshot["complete"] = true
	snapshot["observed_at"] = time.Now().Add(-time.Hour).Unix()
	write()
	if _, _, err = stewardSnapshot(context.Background(), p); err == nil {
		t.Fatal("stale accepted")
	}
	snapshot["observed_at"] = time.Now().Unix()
	snapshot["schema_version"] = "2"
	write()
	if _, _, err = stewardSnapshot(context.Background(), p); err == nil {
		t.Fatal("unknown version accepted")
	}
	if validStewardEndpoint("file://other-host/private.json") == nil {
		t.Fatal("remote file accepted")
	}
	if validateStewardJSON([]byte(`{"complete":true,"complete":false}`)) == nil {
		t.Fatal("duplicate keys accepted")
	}
}

func TestStewardPartialScanPreservesOverridesAndObservations(t *testing.T) {
	s, _, _ := testServer(t)
	path := filepath.Join(t.TempDir(), "export.json")
	uPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uPath, "/") {
		uPath = "/" + uPath
	}
	p := provider{ID: "steward", Name: "Steward", Type: "steward", Endpoint: (&url.URL{Scheme: "file", Path: uPath}).String(), Enabled: true}
	insertProvider(t, s, p)
	value := map[string]any{"schema_version": "1", "kind": "steward.inventory", "host_id": "host", "revision": "1", "observed_at": time.Now().Unix(), "complete": true, "sources": []any{}, "resources": []any{map[string]any{"kind": "service", "id": "worker", "binding": "", "scope": "user", "name": "Worker", "enabled": true, "ownership": "owned_definition", "observed": "running", "public_url": nil}}}
	write := func() {
		t.Helper()
		b, _ := json.Marshal(value)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if err := s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, err := s.apps()
	if err != nil || len(apps) != 1 {
		t.Fatalf("%v %#v", err, apps)
	}
	if _, err = s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"name":"My worker","favorite":true}`, apps[0].ID); err != nil {
		t.Fatal(err)
	}
	value["complete"] = false
	value["resources"] = []any{}
	write()
	if err = s.scan(context.Background(), p); err == nil {
		t.Fatal("partial export scan succeeded")
	}
	apps, err = s.apps()
	if err != nil || len(apps) != 1 || apps[0].Name != "My worker" || !apps[0].Favorite || apps[0].Lifecycle != "present" {
		t.Fatalf("partial input lost existing observation/preferences: %v %#v", err, apps)
	}
}
