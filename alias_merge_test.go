package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraefikInternalRoutersAndHostAliases(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/http/routers":
			reply(w, []map[string]any{
				{"name": "acme-http@internal", "provider": "internal", "service": "acme-http@internal", "rule": "PathPrefix(`/.well-known/acme-challenge/`)", "using": []string{"web"}, "status": "enabled"},
				{"name": "api@internal", "provider": "internal", "service": "api@internal", "rule": "PathPrefix(`/api`)", "using": []string{"traefik"}, "status": "enabled"},
				{"name": "dashboard@internal", "service": "dashboard@internal", "rule": "PathPrefix(`/`)", "using": []string{"traefik"}, "status": "enabled"},
				{"name": "ping@internal", "provider": "internal", "service": "ping@internal", "rule": "PathPrefix(`/ping`)", "using": []string{"traefik"}, "status": "enabled"},
				{"name": "web-to-websecure@internal", "provider": "internal", "service": "noop@internal", "rule": "HostRegexp(`^.+$`)", "using": []string{"web"}, "status": "enabled"},
				{"name": "workspace@file", "provider": "file", "service": "workspace", "rule": "Host(`workspace.node.example.test`)", "using": []string{"websecure"}, "tls": map[string]any{}, "status": "enabled"},
				{"name": "portal@file", "provider": "file", "service": "portal", "rule": "Host(`portal.example.test`) || Host(`portal-alt.example.test`) || Host(`portal.node.example.test`) || Host(`portal-alt.node.example.test`)", "using": []string{"websecure"}, "tls": map[string]any{}, "status": "enabled"},
			})
		case "/api/entrypoints":
			reply(w, []traefikEntryPoint{{Name: "web", Address: ":80"}, {Name: "websecure", Address: ":443"}, {Name: "traefik", Address: ":8080"}})
		case "/api/http/services":
			reply(w, []map[string]any{
				{"name": "workspace@file", "status": "enabled", "serverStatus": map[string]string{"http://workspace:7888": "UP"}},
				{"name": "portal@file", "status": "enabled", "serverStatus": map[string]string{"http://portal:8080": "UP"}},
			})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	s, _, _ := testServer(t)
	p := provider{ID: "traefik", Name: "Traefik", Type: "traefik", Endpoint: api.URL, Enabled: true}
	insertProvider(t, s, p)
	obs, warning, err := traefikSnapshot(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 || warning != "" {
		t.Fatalf("expected two apps without internal-router warnings: %d %q", len(obs), warning)
	}
	var portal observation
	for _, o := range obs {
		if o.Facts["router"].Value == "portal@file" {
			portal = o
		}
	}
	if portal.Facts["url"].Value != "https://portal.example.test/" || len(portal.URLs) != 4 || portal.Facts["health"].Value != "healthy" {
		t.Fatalf("alias ordering/service lookup incorrect: %+v", portal)
	}
	// Recreate the v0.3 registry: four independent alias cards plus three bogus internal cards.
	legacy := []observation{}
	for _, o := range obs {
		for _, raw := range o.URLs {
			old := proxyObservation(p, "legacy:"+raw, o.Facts["name"].Value, raw, "")
			old.Facts["router"] = o.Facts["router"]
			legacy = append(legacy, old)
		}
	}
	for _, name := range []string{"acme-http", "dashboard", "ping"} {
		old := proxyObservation(p, "router:"+name+"@internal/unresolved", name, "", "Cannot resolve hostname")
		old.Facts["router"] = fact{name + "@internal", 20, "Traefik"}
		legacy = append(legacy, old)
	}
	if err = s.reconcile(p.ID, legacy); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	if len(apps) != 8 {
		t.Fatal("legacy fixture did not create eight cards")
	}
	byURL := map[string]string{}
	for _, a := range apps {
		byURL[a.URL] = a.ID
	}
	primary := byURL[portal.URLs[0]]
	other := byURL[portal.URLs[1]]
	third := byURL[portal.URLs[2]]
	workspace := byURL["https://workspace.node.example.test/"]
	if _, err = s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"name":"My portal","favorite":true}`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"hidden":true}`, third); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE dashboards SET items=? WHERE id='home'", encode([]string{workspace, other, third, primary})); err != nil {
		t.Fatal(err)
	}
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, err = s.apps()
	if err != nil || len(apps) != 2 {
		t.Fatalf("did not consolidate old records: %+v (%v)", apps, err)
	}
	merged, err := s.app(primary)
	if err != nil || merged.Name != "My portal" || !merged.Favorite || !merged.Hidden || len(merged.URLs) != 4 {
		t.Fatalf("merge lost preferences or aliases: %+v (%v)", merged, err)
	}
	ds, _ := s.dashboards()
	if encode(ds[0].Items) != encode([]string{workspace, primary}) {
		t.Fatalf("placements/order were lost: %v", ds[0].Items)
	}
	if strings.Contains(encode(publicApp(merged)), "portal-alt") {
		t.Fatal("public response unexpectedly exposed alternate addresses")
	}
	if err = s.scan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	apps, _ = s.apps()
	if len(apps) != 2 {
		t.Fatal("rescan recreated duplicate/internal cards")
	}
}

func TestAliasGroupingKeepsPathsAndPrefersHTTPS(t *testing.T) {
	p := provider{ID: "p", Name: "Traefik", Type: "traefik"}
	obs := []observation{}
	for _, raw := range []string{"http://first.example.test/photos", "https://first.example.test/photos", "https://second.example.test/photos", "https://first.example.test/notes"} {
		o := proxyObservation(p, raw, "files", raw, "")
		o.Facts["router"] = fact{"files@file", 20, "Traefik"}
		obs = append(obs, o)
	}
	groups := groupTraefikAliases(p, obs)
	if len(groups) != 2 {
		t.Fatal("separate paths were merged")
	}
	for _, o := range groups {
		if strings.HasSuffix(o.Facts["url"].Value, "/photos") {
			if o.Facts["url"].Value != "https://first.example.test/photos" || len(o.URLs) != 3 {
				t.Fatalf("HTTPS or aliases lost: %+v", o)
			}
		}
	}
}

func TestAliasMergeRejectsConflictingOverrides(t *testing.T) {
	s, _, _ := testServer(t)
	p := provider{ID: "p", Type: "traefik", Name: "Traefik", Endpoint: "http://example.test"}
	insertProvider(t, s, p)
	legacy := []observation{}
	for _, raw := range []string{"https://a.example.test/", "https://b.example.test/"} {
		o := proxyObservation(p, raw, "App", raw, "")
		o.Facts["router"] = fact{"app@file", 20, "Traefik"}
		legacy = append(legacy, o)
	}
	if err := s.reconcile(p.ID, legacy); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	for i, a := range apps {
		if _, err := s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", encode(map[string]string{"name": []string{"Name A", "Name B"}[i]}), a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.reconcile(p.ID, groupTraefikAliases(p, legacy)); err == nil || !strings.Contains(err.Error(), "conflicting name overrides") {
		t.Fatalf("expected explicit conflict: %v", err)
	}
	apps, _ = s.apps()
	if len(apps) != 2 {
		t.Fatal("conflicting records were discarded")
	}
	for _, a := range apps {
		if a.Lifecycle != "present" {
			t.Fatal("failed merge did not roll back the snapshot")
		}
	}
	// Explicit cross-provider IDs are stronger than the inferred alias relationship.
	for i, a := range apps {
		if _, err := s.db.Exec("UPDATE applications SET overrides='{}' WHERE id=?", a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec("INSERT INTO identities(key,app_id) VALUES(?,?)", []string{"explicit:a", "explicit:b"}[i], a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.reconcile(p.ID, groupTraefikAliases(p, legacy)); err == nil || !strings.Contains(err.Error(), "explicit Apptrail IDs") {
		t.Fatalf("explicit identity conflict ignored: %v", err)
	}
}

func TestInternalCleanupPreservesCuratedAndRealApps(t *testing.T) {
	s, _, _ := testServer(t)
	p := provider{ID: "p", Type: "traefik", Name: "Traefik", Endpoint: "http://example.test"}
	insertProvider(t, s, p)
	obs := []observation{}
	for _, name := range []string{"acme@internal", "ping@internal", "dashboard@internal", "real@file"} {
		o := proxyObservation(p, name, name, "", "Unresolved")
		o.Facts["router"] = fact{name, 20, "Traefik"}
		obs = append(obs, o)
	}
	if err := s.reconcile(p.ID, obs); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	for _, a := range apps {
		switch a.Name {
		case "ping@internal":
			if _, err := s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", `{"favorite":true}`, a.ID); err != nil {
				t.Fatal(err)
			}
		case "dashboard@internal":
			if _, err := s.db.Exec("UPDATE dashboards SET items=? WHERE id='home'", encode([]string{a.ID})); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.reconcile(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	apps, _ = s.apps()
	if len(apps) != 3 {
		t.Fatalf("cleanup discarded curated/real apps: %+v", apps)
	}
	for _, a := range apps {
		if a.Name == "acme@internal" || a.Lifecycle != "missing" {
			t.Fatalf("unexpected cleanup state: %+v", a)
		}
	}
	ds, _ := s.dashboards()
	if len(ds[0].Items) != 1 {
		t.Fatal("curated internal placement was removed")
	}
}
