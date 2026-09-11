package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func testServer(t *testing.T) (*server, *httptest.Server, *http.Client) {
	t.Helper()
	db, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server{db: db, setupToken: "test-setup-token", attempts: map[string]attempt{}}
	ts := httptest.NewServer(s.routes())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	t.Cleanup(func() { ts.Close(); db.Close() })
	return s, ts, client
}
func request(t *testing.T, c *http.Client, method, raw string, data any, status int) []byte {
	t.Helper()
	var b io.Reader
	if data != nil {
		b = strings.NewReader(encode(data))
	}
	req, err := http.NewRequest(method, raw, b)
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, raw, status, res.StatusCode, out)
	}
	return out
}
func loginSetup(t *testing.T, ts *httptest.Server, c *http.Client) {
	t.Helper()
	request(t, c, "POST", ts.URL+"/api/setup", credentials{Username: "owner", Password: "correct horse battery staple", Token: "test-setup-token"}, 200)
}

func TestOwnerPublicationAndRevocation(t *testing.T) {
	s, ts, owner := testServer(t)
	anon := ts.Client()
	request(t, anon, "POST", ts.URL+"/api/setup", credentials{Username: "owner", Password: "correct horse battery staple", Token: "bad"}, 403)
	loginSetup(t, ts, owner)
	request(t, owner, "POST", ts.URL+"/api/setup", credentials{Username: "other", Password: "correct horse battery staple", Token: "test-setup-token"}, 409)
	for _, path := range []string{"/apps", "/providers", "/dashboards", "/settings"} {
		request(t, anon, "GET", ts.URL+"/api"+path, nil, 401)
	}
	request(t, anon, "POST", ts.URL+"/api/apps", map[string]string{"name": "Unauthorized", "url": "https://example.org"}, 401)
	request(t, owner, "POST", ts.URL+"/api/apps", map[string]string{"name": "Bad URL", "url": "javascript:alert(1)"}, 400)
	var a, b application
	if err := json.Unmarshal(request(t, owner, "POST", ts.URL+"/api/apps", map[string]string{"name": "Public app", "url": "https://example.org/photos", "description": "Visible description"}, 200), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(request(t, owner, "POST", ts.URL+"/api/apps", map[string]string{"name": "Private app", "url": "https://example.org/notes"}, 200), &b); err != nil {
		t.Fatal(err)
	}
	d := dashboard{ID: "home", Name: "Shared apps", Slug: "home", Public: true, Items: []string{a.ID}}
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200)
	public := request(t, anon, "GET", ts.URL+"/api/public/home", nil, 200)
	for _, secret := range []string{b.ID, "Private app", "sources", "fields", "created", "overrides", "manual:"} {
		if bytes.Contains(public, []byte(secret)) {
			t.Fatalf("public response leaked %q: %s", secret, public)
		}
	}
	if !bytes.Contains(public, []byte("Public app")) {
		t.Fatal("published app missing")
	}
	request(t, anon, "GET", ts.URL+"/api/icons/"+b.ID, nil, 404)
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/apps/"+a.ID, strings.NewReader(`{"hidden":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.invalid")
	res, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	request(t, owner, "PATCH", ts.URL+"/api/apps/"+a.ID, map[string]bool{"hidden": true}, 200)
	public = request(t, anon, "GET", ts.URL+"/api/public/home", nil, 200)
	if bytes.Contains(public, []byte(a.ID)) {
		t.Fatal("hidden app exposed")
	}
	d.Public = false
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200)
	request(t, anon, "GET", ts.URL+"/api/public/home", nil, 404)
	request(t, owner, "DELETE", ts.URL+"/api/apps/"+a.ID, nil, 200)
	dashboards, err := s.dashboards()
	if err != nil || len(dashboards[0].Items) != 0 {
		t.Fatal("forgotten app left dangling dashboard items")
	}
	request(t, owner, "POST", ts.URL+"/api/account", map[string]string{"current": "wrong", "password": "a different strong password"}, 403)
	jar, _ := cookiejar.New(nil)
	old := &http.Client{Jar: jar}
	request(t, old, "POST", ts.URL+"/api/login", credentials{Username: "owner", Password: "correct horse battery staple"}, 200)
	request(t, owner, "POST", ts.URL+"/api/account", map[string]string{"current": "correct horse battery staple", "password": "a different strong password"}, 200)
	request(t, old, "GET", ts.URL+"/api/apps", nil, 401)
	request(t, owner, "GET", ts.URL+"/api/apps", nil, 200)
	if _, err = s.db.Exec("UPDATE sessions SET expires=0"); err != nil {
		t.Fatal(err)
	}
	request(t, owner, "GET", ts.URL+"/api/apps", nil, 401)
}

func TestDiscoveryIdentityAndFailedScans(t *testing.T) {
	s, _, _ := testServer(t)
	var phase atomic.Int32
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.44/containers/json" {
			t.Errorf("unexpected Docker path: %s", r.URL.Path)
		}
		switch phase.Load() {
		case 2:
			w.WriteHeader(500)
			return
		case 3:
			reply(w, []container{})
			return
		}
		reply(w, []container{{ID: []string{"old-container", "recreated-container"}[phase.Load()], Names: []string{"/photos"}, State: "running", Status: "Up (healthy)", Labels: map[string]string{"com.docker.compose.project": "homelab", "com.docker.compose.service": "photos", "apptrail.id": "photos", "apptrail.name": "Discovered photos", "traefik.http.routers.photos.rule": "Host(`photos.example.org`) && PathPrefix(`/library`)", "traefik.http.routers.photos.tls": "true"}}})
	}))
	defer docker.Close()
	p := provider{ID: "docker-test", Name: "Test Docker", Endpoint: docker.URL, Enabled: true}
	if _, err := s.db.Exec("INSERT INTO providers(id,name,endpoint) VALUES(?,?,?)", p.ID, p.Name, p.Endpoint); err != nil {
		t.Fatal(err)
	}
	if err := s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, err := s.apps()
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps: %v, %v", apps, err)
	}
	a := apps[0]
	if a.URL != "https://photos.example.org/library" {
		t.Fatalf("wrong route %s", a.URL)
	}
	if _, err = s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"name":"My photos","favorite":true}`, a.ID); err != nil {
		t.Fatal(err)
	}
	phase.Store(1)
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, err = s.apps()
	if err != nil || len(apps) != 1 || apps[0].ID != a.ID || apps[0].Name != "My photos" || !apps[0].Favorite {
		t.Fatalf("identity or override lost: %v (%v)", apps, err)
	}
	phase.Store(2)
	if err = s.scan(context.Background(), p); err == nil {
		t.Fatal("expected scan failure")
	}
	apps, err = s.apps()
	if err != nil || apps[0].Lifecycle != "present" {
		t.Fatal("failed scan marked an app missing")
	}
	conflict := observation{Source: "another-app", Keys: []string{"explicit:different", "url:" + a.URL}, Facts: map[string]fact{"name": {"Wrong", 80, "conflict"}}}
	if err = s.reconcile(p.ID, []observation{conflict}); err == nil {
		t.Fatal("distinct explicit identities were merged")
	}
	apps, _ = s.apps()
	if len(apps) != 1 || apps[0].Lifecycle != "present" {
		t.Fatal("conflict did not roll back snapshot")
	}
	phase.Store(3)
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, _ = s.apps()
	if apps[0].Lifecycle != "missing" || apps[0].Name != "My photos" {
		t.Fatal("missing app lost durable preferences")
	}
}

func TestProbePolicyAndManifest(t *testing.T) {
	for _, raw := range []string{"169.254.169.254", "::ffff:169.254.169.254", "fe80::1", "0.0.0.0", "224.0.0.1", "64:ff9b::a9fe:a9fe", "100.100.100.200", "fd00:ec2::254", "168.63.129.16"} {
		if allowedIP(netip.MustParseAddr(raw), true) {
			t.Errorf("allowed blocked address %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.100.100.100", "::1", "fd00::1"} {
		ip := netip.MustParseAddr(raw)
		if allowedIP(ip, false) || !allowedIP(ip, true) {
			t.Errorf("incorrect private policy for %s", raw)
		}
	}
	s, _, _ := testServer(t)
	var head atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notes/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<title>HTML title</title><meta name="description" content="HTML description"><link rel="icon" href="icon.svg">`)
		case "/.well-known/apptrail.json":
			reply(w, map[string]any{"version": "1", "id": "notes-manifest", "name": "Manifest title", "icon": "manifest-icon.svg", "health": map[string]any{"url": "/health", "method": "HEAD", "expected_status": []int{204}}})
		case "/health":
			head.Store(r.Method == "HEAD")
			w.WriteHeader(204)
		case "/redirect":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", 302)
		case "/large":
			_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1))
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	c := probeClient(false)
	defer c.CloseIdleConnections()
	if _, _, err := fetch(context.Background(), c, upstream.URL+"/notes/"); !errors.Is(err, errProbeBlocked) {
		t.Fatalf("expected blocked loopback: %v", err)
	}
	c = probeClient(true)
	defer c.CloseIdleConnections()
	if _, _, err := fetch(context.Background(), c, upstream.URL+"/redirect"); !errors.Is(err, errProbeBlocked) {
		t.Fatalf("redirect bypassed policy: %v", err)
	}
	if _, _, err := fetch(context.Background(), c, upstream.URL+"/large"); err == nil {
		t.Fatal("unbounded response allowed")
	}
	if _, err := s.db.Exec("UPDATE settings SET private_probes=1"); err != nil {
		t.Fatal(err)
	}
	o := observation{Source: "notes", Keys: []string{"url:" + upstream.URL + "/notes"}, Facts: map[string]fact{"name": {"Inferred title", 20, "Docker"}, "url": {upstream.URL + "/notes/", 30, "Traefik"}}}
	if err := s.reconcile("test", []observation{o}); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	a := apps[0]
	if err := s.enrich(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	a, err := s.app(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Manifest title" || a.Description != "HTML description" || a.Icon != upstream.URL+"/notes/manifest-icon.svg" || a.Health != "healthy" || !head.Load() {
		t.Fatalf("incorrect metadata: %+v", a)
	}
	var identity string
	if err = s.db.QueryRow("SELECT app_id FROM identities WHERE key='explicit:notes-manifest'").Scan(&identity); err != nil || identity != a.ID {
		t.Fatal("manifest identity was not retained")
	}
	if _, err = s.db.Exec("UPDATE settings SET private_probes=0"); err != nil {
		t.Fatal(err)
	}
	if err = s.enrich(context.Background(), a.ID); !errors.Is(err, errProbeBlocked) {
		t.Fatalf("expected policy rejection: %v", err)
	}
	a, err = s.app(a.ID)
	if err != nil || a.Name != "Manifest title" || a.Health != "unknown" {
		t.Fatal("blocked probe lost metadata or reported false health")
	}
}

func TestSQLiteRestartAndURLIdentity(t *testing.T) {
	if normalizedURL("https://EXAMPLE.org:443/photos/") != normalizedURL("https://example.org/photos") {
		t.Fatal("URL normalization mismatch")
	}
	if normalizedURL("https://example.org/photos") == normalizedURL("https://example.org/notes") {
		t.Fatal("base paths merged")
	}
	path := filepath.Join(t.TempDir(), "durable.db")
	db, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{db: db}
	o := observation{Source: "manual", Keys: []string{"url:https://example.org"}, Facts: map[string]fact{"name": {"Durable", 70, "manual"}, "url": {"https://example.org", 70, "manual"}}}
	if err = s.reconcile("manual", []observation{o}); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	id := apps[0].ID
	if _, err = db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"favorite":true,"name":"Still here"}`, id); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.db = db
	apps, err = s.apps()
	if err != nil || len(apps) != 1 || apps[0].ID != id || apps[0].Name != "Still here" || !apps[0].Favorite {
		t.Fatalf("restart lost state: %v (%v)", apps, err)
	}
}
