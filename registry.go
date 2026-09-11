package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type fact struct {
	Value    string `json:"value"`
	Priority int    `json:"priority"`
	Source   string `json:"source"`
}
type observation struct {
	Source string          `json:"source"`
	Keys   []string        `json:"keys"`
	Facts  map[string]fact `json:"facts"`
}
type application struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Category    string          `json:"category"`
	Health      string          `json:"health"`
	Lifecycle   string          `json:"lifecycle"`
	Favorite    bool            `json:"favorite"`
	Hidden      bool            `json:"hidden"`
	Created     int64           `json:"created"`
	Seen        int64           `json:"seen"`
	Sources     []string        `json:"sources"`
	Fields      map[string]fact `json:"fields"`
	ProbeError  string          `json:"probe_error,omitempty"`
}
type override struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	URL         *string `json:"url,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Category    *string `json:"category,omitempty"`
	Favorite    *bool   `json:"favorite,omitempty"`
	Hidden      *bool   `json:"hidden,omitempty"`
}

func validURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("Use an http:// or https:// URL without credentials")
	}
	return u, nil
}
func normalizedURL(raw string) string {
	u, err := validURL(raw)
	if err != nil {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String()
}
func (s *server) reconcile(provider string, observations []observation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE observations SET present=0 WHERE provider=?", provider); err != nil {
		return err
	}
	for _, o := range observations {
		appID := ""
		for _, k := range o.Keys {
			var found string
			err = tx.QueryRow("SELECT app_id FROM identities WHERE key=?", k).Scan(&found)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if found != "" {
				if appID != "" && appID != found {
					return fmt.Errorf("conflicting identities for %s; set a consistent apptrail.id", o.Source)
				}
				appID = found
			}
		}
		if appID == "" {
			appID = id()
			if _, err = tx.Exec("INSERT INTO applications(id,created) VALUES(?,?)", appID, time.Now().Unix()); err != nil {
				return err
			}
		}
		for _, key := range o.Keys {
			if strings.HasPrefix(key, "explicit:") {
				var conflicting int
				if err = tx.QueryRow("SELECT COUNT(*) FROM identities WHERE app_id=? AND key LIKE 'explicit:%' AND key!=?", appID, key).Scan(&conflicting); err != nil {
					return err
				}
				if conflicting > 0 {
					return fmt.Errorf("conflicting explicit Apptrail IDs for %s; observations were retained", o.Source)
				}
			}
		}
		for _, k := range o.Keys {
			if _, err = tx.Exec("INSERT OR IGNORE INTO identities(key,app_id) VALUES(?,?)", k, appID); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("INSERT INTO observations(provider,source,app_id,payload,present,seen) VALUES(?,?,?,?,1,?) ON CONFLICT(provider,source) DO UPDATE SET app_id=excluded.app_id,payload=excluded.payload,present=1,seen=excluded.seen", provider, o.Source, appID, encode(o), time.Now().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *server) apps() ([]application, error) {
	rows, err := s.db.Query("SELECT id,created,overrides FROM applications ORDER BY created,id")
	if err != nil {
		return nil, err
	}
	apps := []application{}
	overrides := map[string]override{}
	positions := map[string]int{}
	for rows.Next() {
		var a application
		var raw string
		if err = rows.Scan(&a.ID, &a.Created, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var ov override
		if err = json.Unmarshal([]byte(raw), &ov); err != nil {
			rows.Close()
			return nil, err
		}
		overrides[a.ID] = ov
		a.Fields = map[string]fact{}
		a.Sources = []string{}
		a.Health = "unknown"
		a.Lifecycle = "missing"
		positions[a.ID] = len(apps)
		apps = append(apps, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.Query("SELECT provider,payload,present,seen,app_id FROM observations ORDER BY present ASC,seen ASC,provider,source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var provider, raw, appID string
		var present bool
		var seen int64
		if err = rows.Scan(&provider, &raw, &present, &seen, &appID); err != nil {
			return nil, err
		}
		i, ok := positions[appID]
		if !ok {
			continue
		}
		a := &apps[i]
		var o observation
		if err = json.Unmarshal([]byte(raw), &o); err != nil {
			return nil, err
		}
		if provider != "http" {
			if !contains(a.Sources, provider) {
				a.Sources = append(a.Sources, provider)
			}
			if present {
				a.Lifecycle = "present"
			}
			if seen > a.Seen {
				a.Seen = seen
			}
		}
		for key, f := range o.Facts {
			old, ok := a.Fields[key]
			if !ok || f.Priority >= old.Priority {
				a.Fields[key] = f
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range apps {
		a := &apps[i]
		ov := overrides[a.ID]
		for key, p := range map[string]*string{"name": ov.Name, "description": ov.Description, "url": ov.URL, "icon": ov.Icon, "category": ov.Category} {
			if p != nil {
				a.Fields[key] = fact{*p, 100, "user override"}
			}
		}
		a.Name = a.Fields["name"].Value
		a.Description = a.Fields["description"].Value
		a.URL = a.Fields["url"].Value
		a.Icon = a.Fields["icon"].Value
		a.Category = a.Fields["category"].Value
		if a.Category == "" {
			a.Category = "Other"
		}
		if h := a.Fields["health"].Value; h != "" {
			a.Health = h
		}
		a.ProbeError = a.Fields["probe_error"].Value
		if a.Name == "" {
			a.Name = "Unnamed application"
		}
		if ov.Favorite != nil {
			a.Favorite = *ov.Favorite
		}
		if ov.Hidden != nil {
			a.Hidden = *ov.Hidden
		}
		if a.Lifecycle == "missing" && time.Now().Unix()-a.Seen > 86400 {
			a.Lifecycle = "stale"
		}
	}
	return apps, nil
}
func contains(items []string, id string) bool {
	for _, v := range items {
		if v == id {
			return true
		}
	}
	return false
}
func (s *server) app(appID string) (application, error) {
	// ponytail: assemble the small homelab registry in memory; filter SQL by app_id if per-icon/probe reads become expensive.
	apps, err := s.apps()
	if err != nil {
		return application{}, err
	}
	for _, a := range apps {
		if a.ID == appID {
			return a, nil
		}
	}
	return application{}, sql.ErrNoRows
}
func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	a, err := s.apps()
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, a)
}
func (s *server) createApp(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Icon        string `json:"icon"`
	}
	if !body(w, r, &b) {
		return
	}
	if !required(b.Name, 120) || len(b.Description) > 1000 || len(b.Category) > 60 {
		fail(w, 400, "Enter a name (up to 120 characters), description (1000), and category (60)")
		return
	}
	if _, err := validURL(b.URL); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.Icon != "" {
		if _, err := validURL(b.Icon); err != nil {
			fail(w, 400, "Icon must be an HTTP or HTTPS URL")
			return
		}
	}
	// Manual entries use URL identity too, so later infrastructure discovery enriches the same app.
	o := observation{Source: id(), Keys: []string{"url:" + normalizedURL(b.URL)}, Facts: map[string]fact{}}
	for k, v := range map[string]string{"name": b.Name, "url": b.URL, "description": b.Description, "category": b.Category, "icon": b.Icon} {
		if v != "" {
			o.Facts[k] = fact{v, 70, "manual entry"}
		}
	}
	// Each manual entry is its own snapshot scope; editing one never marks another missing.
	if err := s.reconcile("manual:"+o.Source, []observation{o}); err != nil {
		dbError(w, err)
		return
	}
	var appID string
	if err := s.db.QueryRow("SELECT app_id FROM identities WHERE key=?", o.Keys[0]).Scan(&appID); err != nil {
		dbError(w, err)
		return
	}
	a, err := s.app(appID)
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, a)
}
func (s *server) updateApp(w http.ResponseWriter, r *http.Request) {
	var patch override
	if !body(w, r, &patch) {
		return
	}
	appID := r.PathValue("id")
	var raw string
	if err := s.db.QueryRow("SELECT overrides FROM applications WHERE id=?", appID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			notFound(w)
		} else {
			dbError(w, err)
		}
		return
	}
	var ov override
	if err := json.Unmarshal([]byte(raw), &ov); err != nil {
		dbError(w, err)
		return
	}
	for k, p := range map[string]*string{"name": patch.Name, "description": patch.Description, "category": patch.Category, "url": patch.URL, "icon": patch.Icon} {
		if p == nil {
			continue
		}
		if len(*p) > 2000 {
			fail(w, 400, "Value is too long")
			return
		}
		if (k == "url" || k == "icon") && *p != "" {
			if _, err := validURL(*p); err != nil {
				fail(w, 400, err.Error())
				return
			}
		}
		if k == "name" && !required(*p, 120) {
			fail(w, 400, "Name is required (up to 120 characters)")
			return
		}
	}
	if patch.URL != nil && *patch.URL == "" {
		fail(w, 400, "Launch URL is required")
		return
	}
	if patch.Name != nil {
		ov.Name = patch.Name
	}
	if patch.URL != nil {
		ov.URL = patch.URL
	}
	if patch.Description != nil {
		ov.Description = patch.Description
	}
	if patch.Icon != nil {
		ov.Icon = patch.Icon
	}
	if patch.Category != nil {
		ov.Category = patch.Category
	}
	if patch.Favorite != nil {
		ov.Favorite = patch.Favorite
	}
	if patch.Hidden != nil {
		ov.Hidden = patch.Hidden
	}
	res, err := s.db.Exec("UPDATE applications SET overrides=? WHERE id=?", encode(ov), appID)
	if !changed(w, res, err) {
		return
	}
	a, err := s.app(appID)
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, a)
}
func (s *server) deleteApp(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.Begin()
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM applications WHERE id=?", r.PathValue("id"))
	if !changed(w, res, err) {
		return
	}
	if _, err = tx.Exec("UPDATE dashboards SET items=(SELECT json_group_array(value) FROM json_each(dashboards.items) WHERE value!=?)", r.PathValue("id")); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}

type dashboard struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Slug   string   `json:"slug"`
	Public bool     `json:"public"`
	Items  []string `json:"items"`
}

func (s *server) dashboards() ([]dashboard, error) {
	rows, err := s.db.Query("SELECT id,name,slug,public,items FROM dashboards ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dashboard{}
	for rows.Next() {
		var d dashboard
		var raw string
		if err = rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Public, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &d.Items); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *server) listDashboards(w http.ResponseWriter, r *http.Request) {
	d, err := s.dashboards()
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, d)
}
func validSlug(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func (s *server) saveDashboard(w http.ResponseWriter, r *http.Request) {
	var d dashboard
	if !body(w, r, &d) {
		return
	}
	if !required(d.Name, 80) || !validSlug(d.Slug) || len(d.Items) > 1000 {
		fail(w, 400, "Provide a name and a slug using lowercase letters, numbers, or hyphens")
		return
	}
	if d.Items == nil {
		d.Items = []string{}
	}
	apps, err := s.apps()
	if err != nil {
		dbError(w, err)
		return
	}
	known := map[string]bool{}
	for _, a := range apps {
		known[a.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range d.Items {
		if !known[id] || seen[id] {
			fail(w, 400, "Dashboard contains duplicate or unknown apps")
			return
		}
		seen[id] = true
	}
	var duplicate int
	err = s.db.QueryRow("SELECT COUNT(*) FROM dashboards WHERE slug=? AND id!=?", d.Slug, r.PathValue("id")).Scan(&duplicate)
	if err != nil {
		dbError(w, err)
		return
	}
	if duplicate > 0 {
		fail(w, 409, "That public URL is already in use")
		return
	}
	if r.Method == "POST" {
		d.ID = id()
		_, err = s.db.Exec("INSERT INTO dashboards(id,name,slug,public,items) VALUES(?,?,?,?,?)", d.ID, strings.TrimSpace(d.Name), d.Slug, d.Public, encode(d.Items))
	} else {
		d.ID = r.PathValue("id")
		var res sql.Result
		res, err = s.db.Exec("UPDATE dashboards SET name=?,slug=?,public=?,items=? WHERE id=?", strings.TrimSpace(d.Name), d.Slug, d.Public, encode(d.Items), d.ID)
		if !changed(w, res, err) {
			return
		}
	}
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, d)
}
func (s *server) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == "home" {
		fail(w, 400, "Keep the default dashboard; you can rename it instead")
		return
	}
	res, err := s.db.Exec("DELETE FROM dashboards WHERE id=?", r.PathValue("id"))
	if !changed(w, res, err) {
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func publicApp(a application) map[string]any {
	health := a.Health
	if a.Lifecycle == "missing" || a.Lifecycle == "stale" {
		health = a.Lifecycle
	}
	return map[string]any{"id": a.ID, "name": a.Name, "description": a.Description, "url": a.URL, "icon": a.Icon != "", "category": a.Category, "health": health}
}
func (s *server) publicDashboard(w http.ResponseWriter, r *http.Request) {
	ds, err := s.dashboards()
	if err != nil {
		dbError(w, err)
		return
	}
	var d *dashboard
	for i := range ds {
		if ds[i].Slug == r.PathValue("slug") && ds[i].Public {
			d = &ds[i]
			break
		}
	}
	if d == nil {
		notFound(w)
		return
	}
	apps, err := s.apps()
	if err != nil {
		dbError(w, err)
		return
	}
	byID := map[string]application{}
	for _, a := range apps {
		if !a.Hidden {
			byID[a.ID] = a
		}
	}
	out := []map[string]any{}
	for _, id := range d.Items {
		if a, ok := byID[id]; ok {
			out = append(out, publicApp(a))
		}
	}
	reply(w, map[string]any{"name": d.Name, "slug": d.Slug, "apps": out})
}
func (s *server) isPublicApp(appID string) bool {
	ds, err := s.dashboards()
	if err != nil {
		return false
	}
	for _, d := range ds {
		if d.Public && contains(d.Items, appID) {
			return true
		}
	}
	return false
}
func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	var p bool
	if err := s.db.QueryRow("SELECT private_probes FROM settings WHERE id=1").Scan(&p); err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]any{"private_probes": p, "scan_interval_seconds": 300})
}
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Private bool `json:"private_probes"`
	}
	if !body(w, r, &b) {
		return
	}
	if _, err := s.db.Exec("UPDATE settings SET private_probes=? WHERE id=1", b.Private); err != nil {
		dbError(w, err)
		return
	}
	s.getSettings(w, r)
}
