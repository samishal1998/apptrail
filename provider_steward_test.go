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
}
