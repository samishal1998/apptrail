package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func dashboardByID(t *testing.T, s *server, id string) dashboard {
	t.Helper()
	ds, err := s.dashboards()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			return d
		}
	}
	t.Fatal("dashboard missing")
	return dashboard{}
}
func saveTestDashboard(t *testing.T, s *server, d dashboard) {
	t.Helper()
	if err := normalizeLayout(&d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE dashboards SET items=?,layout=?,auto_rule=?,auto_section=?,excluded=? WHERE id=?", encode(d.Items), encode(d.Layout), *d.AutoRule, d.AutoSection, encode(d.Excluded), d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCELAutoAddAndManualExclusions(t *testing.T) {
	s, ts, owner := testServer(t)
	loginSetup(t, ts, owner)
	p := provider{ID: "proxy", Type: "traefik", Name: "Traefik", Endpoint: "http://example.test"}
	insertProvider(t, s, p)
	o := observation{Source: "photos", Keys: []string{"explicit:photos"}, Facts: map[string]fact{"name": {"Photos", 20, "Traefik"}, "url": {"https://photos.example.test", 35, "Traefik"}, "category": {"Media", 20, "Traefik"}}}
	if err := s.reconcile(p.ID, []observation{o}); err != nil {
		t.Fatal(err)
	}
	apps, _ := s.apps()
	appID := apps[0].ID
	rule := `url != "" && "traefik" in provider_types && host.endsWith(".example.test")`
	request(t, ts.Client(), "POST", ts.URL+"/api/dashboards/rule-preview", map[string]string{"rule": rule}, 401)
	preview := request(t, owner, "POST", ts.URL+"/api/dashboards/rule-preview", map[string]string{"rule": rule}, 200)
	if !strings.Contains(string(preview), "Photos") {
		t.Fatal("preview omitted matching app")
	}
	d := dashboardByID(t, s, "home")
	d.AutoRule = &rule
	d.Layout.Sections = append(d.Layout.Sections, dashboardSection{ID: "media", Name: "Media"})
	d.AutoSection = "media"
	if err := json.Unmarshal(request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 1 || d.Items[0] != appID || d.Layout.Tiles[appID].Section != "media" {
		t.Fatalf("rule did not add to target section: %+v", d)
	}
	before := d.Layout.Tiles[appID]
	before.X = 6
	before.W = 6
	d.Layout.Tiles[appID] = before
	if err := json.Unmarshal(request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200), &d); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcile(p.ID, []observation{o}); err != nil {
		t.Fatal(err)
	}
	again := dashboardByID(t, s, "home")
	if len(again.Items) != 1 || again.Layout.Tiles[appID] != before {
		t.Fatal("rescan duplicated or moved an existing app")
	}
	d = again
	d.Items = []string{}
	if err := json.Unmarshal(request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200), &d); err != nil {
		t.Fatal(err)
	}
	if !contains(d.Excluded, appID) || len(d.Items) != 0 {
		t.Fatal("manual removal was immediately reversed")
	}
	if err := s.reconcile(p.ID, []observation{o}); err != nil {
		t.Fatal(err)
	}
	if len(dashboardByID(t, s, "home").Items) != 0 {
		t.Fatal("scan ignored exclusion")
	}
	d = dashboardByID(t, s, "home")
	d.Excluded = []string{}
	if err := json.Unmarshal(request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 1 {
		t.Fatal("resetting exclusions did not re-add match")
	}
	// Rule changes are additive, and never remove a previously selected card.
	rule = `category == "Other"`
	d.AutoRule = &rule
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200)
	if len(dashboardByID(t, s, "home").Items) != 1 {
		t.Fatal("changing the rule removed existing content")
	}
}

func TestCELValidationBudgetsAndEligibility(t *testing.T) {
	for _, rule := range []string{`url`, `unknown_field == true`, `url !=`, strings.Repeat(" ", 4097) + "true"} {
		if _, err := compileDashboardRule(rule); err == nil {
			t.Fatalf("accepted invalid rule: %q", rule)
		}
	}
	apps := []application{{ID: "visible", Name: "Visible", Lifecycle: "present", URL: "https://example.test", Favorite: true}, {ID: "hidden", Name: "Hidden", Lifecycle: "present", Hidden: true}, {ID: "missing", Name: "Missing", Lifecycle: "missing"}}
	match, err := evaluateDashboardRule(context.Background(), "true", apps, nil)
	if err != nil || len(match) != 1 || match[0].ID != "visible" {
		t.Fatalf("eligibility is incorrect: %+v %v", match, err)
	}
	match, err = evaluateDashboardRule(context.Background(), "favorite", apps, nil)
	if err != nil || len(match) != 1 {
		t.Fatal("typed boolean expression rejected")
	}
	if _, err = evaluateDashboardRule(context.Background(), `facts.router == "missing"`, apps, nil); err == nil {
		t.Fatal("runtime map error was swallowed")
	}
	urls := []string{}
	for i := 0; i < 300; i++ {
		urls = append(urls, fmt.Sprintf("https://%d.example.test", i))
	}
	apps[0].URLs = urls
	if _, err = evaluateDashboardRule(context.Background(), `urls.all(u, urls.all(v, u == v || u != v))`, apps, nil); err == nil || !strings.Contains(err.Error(), "cost") {
		t.Fatalf("cost budget not enforced: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = evaluateDashboardRule(cancelled, "true", apps, nil); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestRulesDoNotPartiallyPublishOnEvaluationFailure(t *testing.T) {
	s, _, _ := testServer(t)
	p := provider{ID: "proxy", Type: "traefik", Name: "Traefik", Endpoint: "http://example.test"}
	insertProvider(t, s, p)
	d := dashboardByID(t, s, "home")
	rule := `facts.router == "photos"`
	d.AutoRule = &rule
	saveTestDashboard(t, s, d)
	obs := []observation{{Source: "a", Keys: []string{"a"}, Facts: map[string]fact{"name": {"Photos", 20, "proxy"}, "router": {"photos", 20, "proxy"}}}, {Source: "b", Keys: []string{"b"}, Facts: map[string]fact{"name": {"No router", 20, "proxy"}}}}
	if err := s.reconcile(p.ID, obs); err != nil {
		t.Fatal(err)
	}
	d = dashboardByID(t, s, "home")
	if len(d.Items) != 0 || d.RuleError == "" {
		t.Fatal("partially applied a failing rule")
	}
}

func TestDashboardLayoutValidationConcurrencyAndPublicProjection(t *testing.T) {
	s, ts, owner := testServer(t)
	loginSetup(t, ts, owner)
	var a, b application
	json.Unmarshal(request(t, owner, "POST", ts.URL+"/api/apps", map[string]string{"name": "Public", "url": "https://public.example.test"}, 200), &a)
	json.Unmarshal(request(t, owner, "POST", ts.URL+"/api/apps", map[string]string{"name": "Private", "url": "https://private.example.test"}, 200), &b)
	d := dashboardByID(t, s, "home")
	d.Items = []string{a.ID, b.ID}
	d.Public = true
	d.Layout = &dashboardLayout{Sections: []dashboardSection{{ID: "main", Name: "General"}, {ID: "private", Name: "Private section"}}, Tiles: map[string]dashboardTile{a.ID: {Section: "main", W: 6, H: 6}, b.ID: {Section: "private", W: 4, H: 6}}}
	stale := d
	oldRevision := *d.Revision
	stale.Revision = &oldRevision
	if err := json.Unmarshal(request(t, owner, "PUT", ts.URL+"/api/dashboards/home", d, 200), &d); err != nil {
		t.Fatal(err)
	}
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", stale, 409)
	bad := d
	bad.Layout = &dashboardLayout{Sections: d.Layout.Sections, Tiles: map[string]dashboardTile{a.ID: {Section: "main", X: 10, W: 6, H: 6}, b.ID: d.Layout.Tiles[b.ID]}}
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", bad, 400)
	bad.Layout.Tiles[a.ID] = dashboardTile{Section: "private", W: 4, H: 6}
	request(t, owner, "PUT", ts.URL+"/api/dashboards/home", bad, 400)
	request(t, owner, "PATCH", ts.URL+"/api/apps/"+b.ID, map[string]bool{"hidden": true}, 200)
	public := request(t, ts.Client(), "GET", ts.URL+"/api/public/home", nil, 200)
	for _, secret := range []string{b.ID, "Private section", "auto_rule", "excluded", "revision"} {
		if strings.Contains(string(public), secret) {
			t.Fatalf("public layout leaked %s", secret)
		}
	}
	if !strings.Contains(string(public), `"w":6`) {
		t.Fatal("public layout lost geometry")
	}
	request(t, owner, "DELETE", ts.URL+"/api/apps/"+a.ID, nil, 200)
	d = dashboardByID(t, s, "home")
	if _, ok := d.Layout.Tiles[a.ID]; ok {
		t.Fatal("deleted app left a tile behind")
	}
}
