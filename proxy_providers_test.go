package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func insertProvider(t *testing.T, s *server, p provider) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO providers(id,name,endpoint,kind,auth_file) VALUES(?,?,?,?,?)", p.ID, p.Name, p.Endpoint, p.Type, p.AuthFile); err != nil {
		t.Fatal(err)
	}
}

func TestProviderMigrationPreservesExistingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE providers(id TEXT PRIMARY KEY,name TEXT NOT NULL,endpoint TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,scanned INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',count INTEGER NOT NULL DEFAULT 0);
INSERT INTO providers(id,name,endpoint,scanned,count) VALUES('existing','My Docker','unix:///var/run/docker.sock',123,9);
CREATE TABLE applications(id TEXT PRIMARY KEY,created INTEGER NOT NULL,overrides TEXT NOT NULL DEFAULT '{}');
INSERT INTO applications(id,created,overrides) VALUES('app',123,'{"name":"My photos","favorite":true}');
CREATE TABLE owner(id INTEGER PRIMARY KEY CHECK(id=1),username TEXT NOT NULL,password TEXT NOT NULL);
INSERT INTO owner(id,username,password) VALUES(1,'existing-owner','unchanged-password-hash');
CREATE TABLE dashboards(id TEXT PRIMARY KEY,name TEXT NOT NULL,slug TEXT NOT NULL UNIQUE,public INTEGER NOT NULL DEFAULT 0,items TEXT NOT NULL DEFAULT '[]');
INSERT INTO dashboards(id,name,slug,items) VALUES('home','My page','home','["app"]');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = openStore(path)
		if err != nil {
			t.Fatal(err)
		}
		s := &server{db: db}
		ps, err := s.providers()
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 1 || ps[0].Type != "docker" || ps[0].AuthFile != "" || ps[0].Scanned != 123 || ps[0].Count != 9 {
			t.Fatalf("provider changed during upgrade: %+v", ps)
		}
		apps, err := s.apps()
		if err != nil || len(apps) != 1 || apps[0].Name != "My photos" || !apps[0].Favorite {
			t.Fatalf("overrides lost: %+v, %v", apps, err)
		}
		ds, err := s.dashboards()
		if err != nil || ds[0].Name != "My page" || len(ds[0].Items) != 1 {
			t.Fatal("dashboard changed during migration")
		}
		var owner, hash string
		if err = db.QueryRow("SELECT username,password FROM owner WHERE id=1").Scan(&owner, &hash); err != nil || owner != "existing-owner" || hash != "unchanged-password-hash" {
			t.Fatal("owner account changed during migration")
		}
		db.Close()
	}
}

func TestCaddyDiscoveryAndAuthorization(t *testing.T) {
	s, _, _ := testServer(t)
	var config atomic.Value
	var authorization atomic.Value
	authorization.Store("Bearer test-provider-secret")
	config.Store(`{"apps":{"http":{"servers":{
"secure":{"listen":[":443"],"routes":[{"match":[{"host":["photos.example.test"]}],"handle":[{"handler":"subroute","routes":[{"match":[{"path":["/library/*"]}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"rewrite","strip_path_prefix":"/library"}]},{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"10.0.0.2:2342"}]}]}]}]}]}]}]},
"plain":{"listen":[":8080"],"automatic_https":{"disable":true},"routes":[{"match":[{"host":["notes.example.test"]}],"handle":[{"handler":"file_server"}]}]},
"dynamic":{"listen":[":443"],"routes":[{"match":[{"host":["*.example.test"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"dynamic:80"}]}]}]}
}}}}`)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/admin/config/" {
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != authorization.Load().(string) {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, "secret must not enter diagnostics")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, config.Load().(string))
	}))
	defer api.Close()
	file := filepath.Join(t.TempDir(), "authorization")
	if err := os.WriteFile(file, []byte(authorization.Load().(string)), 0600); err != nil {
		t.Fatal(err)
	}
	p := provider{ID: "caddy", Name: "Caddy", Type: "caddy", Endpoint: api.URL + "/admin", Enabled: true, AuthFile: file}
	insertProvider(t, s, p)
	if err := s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, err := s.apps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 3 {
		t.Fatalf("expected 2 apps and one unresolved route: %+v", apps)
	}
	var photos application
	for _, a := range apps {
		switch a.URL {
		case "https://photos.example.test/library/":
			photos = a
		case "http://notes.example.test:8080/":
		case "":
			if a.Fields["discovery_warning"].Value == "" {
				t.Fatal("unresolved route lacks diagnostics")
			}
		default:
			t.Fatalf("unexpected URL %s", a.URL)
		}
	}
	if photos.ID == "" || photos.Fields["backend"].Value != "10.0.0.2:2342" {
		t.Fatalf("incorrect Caddy fields: %+v", photos)
	}
	if _, err = s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"name":"Our library","favorite":true}`, photos.ID); err != nil {
		t.Fatal(err)
	}
	config.Store(strings.Replace(config.Load().(string), "10.0.0.2:2342", "10.0.0.9:2342", 1))
	authorization.Store("Bearer rotated-provider-secret")
	if err = os.WriteFile(file, []byte(authorization.Load().(string)), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	photos, err = s.app(photos.ID)
	if err != nil || photos.Name != "Our library" || !photos.Favorite || photos.Fields["backend"].Value != "10.0.0.9:2342" {
		t.Fatalf("Caddy refresh lost identity or overrides: %+v (%v)", photos, err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM observations WHERE provider='caddy'").Scan(&count); err != nil || count != 3 {
		t.Fatalf("refresh created duplicate observations: %d (%v)", count, err)
	}
	if err = os.WriteFile(file, []byte("Bearer wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.scan(context.Background(), p); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected auth failure: %v", err)
	}
	ps, _ := s.providers()
	if strings.Contains(encode(ps), "rotated-provider-secret") || strings.Contains(ps[0].Error, "secret must not") {
		t.Fatal("credential or response body leaked")
	}
	photos, _ = s.app(photos.ID)
	if photos.Lifecycle != "present" {
		t.Fatal("failed scan marked app missing")
	}
	if err = os.WriteFile(file, []byte(authorization.Load().(string)), 0600); err != nil {
		t.Fatal(err)
	}
	config.Store(`{"status":"not a Caddy API"}`)
	if err = s.scan(context.Background(), p); err == nil {
		t.Fatal("accepted an unrelated API response")
	}
	config.Store(`{"apps":{"http":{"servers":{}}}}`)
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	photos, _ = s.app(photos.ID)
	if photos.Lifecycle != "missing" || !photos.Favorite {
		t.Fatal("empty successful snapshot did not retain missing app preferences")
	}
}

func TestTraefikDiscoveryMergingAndTruncation(t *testing.T) {
	s, _, _ := testServer(t)
	var truncated atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("per_page") != "10001" {
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("X-Next-Page", "1")
		if truncated.Load() {
			w.Header().Set("X-Next-Page", "2")
		}
		var v any
		switch r.URL.Path {
		case "/proxy/api/http/routers":
			v = []map[string]any{
				{"name": "photos@file", "rule": "(Host(`photos.example.test`) || Host(`alias.example.test`)) && PathPrefix(`/library`)", "service": "photos@file", "entryPoints": []string{"secure"}, "tls": map[string]any{}, "status": "enabled"},
				{"name": "notes@docker", "rule": "Host(`notes.example.test`)", "service": "notes@docker", "using": []string{"web"}, "status": "enabled"},
				{"name": "dynamic@file", "rule": "HostRegexp(`.+.example.test`)", "entryPoints": []string{"web"}, "status": "enabled"},
				{"name": "off@file", "rule": "Host(`off.example.test`)", "entryPoints": []string{"web"}, "status": "disabled"},
			}
		case "/proxy/api/entrypoints":
			v = []map[string]any{{"name": "secure", "address": ":8443/tcp"}, {"name": "web", "address": ":8080/TCP"}}
		case "/proxy/api/http/services":
			v = []map[string]any{{"name": "photos@file", "status": "enabled", "loadBalancer": map[string]any{"servers": []map[string]string{{"url": "http://photos:3000"}}}, "serverStatus": map[string]string{"http://photos:3000": "UP"}}, {"name": "notes@docker", "status": "enabled"}}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		reply(w, v)
	}))
	defer api.Close()
	url := "https://photos.example.test:8443/library"
	o := observation{Source: "photos", Keys: []string{"url:" + url}, Facts: map[string]fact{"name": {"Photos", 20, "Docker"}, "url": {url, 30, "Docker labels"}}}
	if err := s.reconcile("docker", []observation{o}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.apps()
	id := before[0].ID
	p := provider{ID: "traefik", Name: "Traefik", Type: "traefik", Endpoint: api.URL + "/proxy", Enabled: true}
	insertProvider(t, s, p)
	if err := s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	if len(apps) != 4 {
		t.Fatalf("expected 3 resolved apps and one unresolved route: %+v", apps)
	}
	photos, err := s.app(id)
	if err != nil || len(photos.Sources) != 2 || photos.Health != "healthy" || photos.Fields["backend"].Value != "http://photos:3000" {
		t.Fatalf("did not merge Docker and Traefik observations: %+v (%v)", photos, err)
	}
	truncated.Store(true)
	if err = s.scan(context.Background(), p); err == nil || !strings.Contains(err.Error(), "paginated") {
		t.Fatalf("did not reject truncated response: %v", err)
	}
	apps, _ = s.apps()
	for _, a := range apps {
		if a.Lifecycle != "present" {
			t.Fatal("truncated scan marked an app missing")
		}
	}
}

func TestProviderRedirectsAndRouteConstraints(t *testing.T) {
	for _, address := range []string{"unix//tmp/proxy:443", ":443/udp"} {
		if _, ok := listenerPort(address); ok {
			t.Fatalf("accepted a non-TCP listener: %s", address)
		}
	}
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true); reply(w, map[string]any{}) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	f := filepath.Join(t.TempDir(), "auth")
	if err := os.WriteFile(f, []byte("Bearer private"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := newProviderClient(provider{Type: "caddy", Endpoint: source.URL, AuthFile: f})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out any
	if err = c.Get(context.Background(), "/config/", &out); err == nil || leaked.Load() {
		t.Fatal("provider followed an authenticated redirect")
	}
	if _, err = traefikMatches("Host(`example.test`) && Method(`POST`)"); err == nil {
		t.Fatal("unsupported constraints were dropped")
	}
	matches, err := traefikMatches("Host(`example.test`) && Path(`/a`) && PathPrefix(`/a`) && PathPrefix(`/a/b`)")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatal("exact-path intersection was incorrectly broadened")
	}
	matches, err = caddyMatches([]routeMatch{{Host: "example.test"}}, []map[string]json.RawMessage{{"host": json.RawMessage(`[]`)}})
	if err != nil || len(matches) != 0 {
		t.Fatal("empty Caddy host matcher became a catch-all")
	}
}

func TestProviderTypeAndAuthConfigurationAPI(t *testing.T) {
	_, ts, owner := testServer(t)
	loginSetup(t, ts, owner)
	b := map[string]any{"type": "caddy", "name": "Caddy", "endpoint": "http://caddy:2019", "enabled": true, "auth_file": ""}
	var created map[string]string
	if err := json.Unmarshal(request(t, owner, "POST", ts.URL+"/api/providers", b, 200), &created); err != nil {
		t.Fatal(err)
	}
	b["type"] = "traefik"
	request(t, owner, "PATCH", ts.URL+"/api/providers/"+created["id"], b, 400)
	b["type"] = "unsupported"
	request(t, owner, "POST", ts.URL+"/api/providers", b, 400)
	b["type"] = "caddy"
	b["endpoint"] = "http://user:password@caddy:2019"
	request(t, owner, "POST", ts.URL+"/api/providers", b, 400)
	b["endpoint"] = "http://caddy:2019"
	b["auth_file"] = "relative-secret"
	request(t, owner, "POST", ts.URL+"/api/providers", b, 400)
}

func TestMissingProxyDoesNotOverrideActiveProvider(t *testing.T) {
	s, _, _ := testServer(t)
	proxy := observation{Source: "router", Keys: []string{"explicit:photos"}, Facts: map[string]fact{"name": {"Proxy name", 35, "Caddy"}, "url": {"https://old.example.test", 35, "Caddy"}}}
	docker := observation{Source: "container", Keys: []string{"explicit:photos"}, Facts: map[string]fact{"name": {"Container name", 20, "Docker"}, "url": {"https://current.example.test", 30, "Docker"}}}
	if err := s.reconcile("caddy", []observation{proxy}); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile("docker", []observation{docker}); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile("caddy", nil); err != nil {
		t.Fatal(err)
	}
	apps, err := s.apps()
	if err != nil || len(apps) != 1 || apps[0].URL != "https://current.example.test" || apps[0].Lifecycle != "present" {
		t.Fatalf("missing source overrode active discovery: %+v (%v)", apps, err)
	}
	if err := s.reconcile("docker", nil); err != nil {
		t.Fatal(err)
	}
	apps, err = s.apps()
	if err != nil || apps[0].URL == "" || apps[0].Lifecycle != "missing" {
		t.Fatalf("fully missing app lost last-known fields: %+v (%v)", apps, err)
	}
}
