package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Type     string `json:"type"`
	AuthFile string `json:"auth_file"`
	Enabled  bool   `json:"enabled"`
	Scanned  int64  `json:"scanned"`
	Error    string `json:"error"`
	Count    int    `json:"count"`
}

func (s *server) providers() ([]provider, error) {
	rows, err := s.db.Query("SELECT id,name,endpoint,enabled,scanned,error,count,kind,auth_file FROM providers ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []provider{}
	for rows.Next() {
		var p provider
		if err = rows.Scan(&p.ID, &p.Name, &p.Endpoint, &p.Enabled, &p.Scanned, &p.Error, &p.Count, &p.Type, &p.AuthFile); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *server) listProviders(w http.ResponseWriter, r *http.Request) {
	p, err := s.providers()
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, p)
}
func validEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("use a base URL without credentials, query parameters, or fragments")
	}
	if u.Scheme == "unix" && u.Host == "" && filepath.IsAbs(u.Path) && u.RawQuery == "" && u.Fragment == "" {
		return nil
	}
	_, err = validURL(raw)
	return err
}
func (s *server) saveProvider(w http.ResponseWriter, r *http.Request) {
	if !s.scans.TryLock() {
		fail(w, 409, "Wait for the active scan to finish")
		return
	}
	defer s.scans.Unlock()
	var p struct {
		Name     string `json:"name"`
		Endpoint string `json:"endpoint"`
		Enabled  bool   `json:"enabled"`
		Type     string `json:"type"`
		AuthFile string `json:"auth_file"`
	}
	if !body(w, r, &p) {
		return
	}
	if !required(p.Name, 100) {
		fail(w, 400, "Provider name is required")
		return
	}
	if p.Type == "" {
		p.Type = "docker"
	}
	if p.Type != "docker" && p.Type != "caddy" && p.Type != "traefik" {
		fail(w, 400, "Choose Docker, Caddy, or Traefik")
		return
	}
	if len(p.Endpoint) > 2048 {
		fail(w, 400, "Provider endpoint is too long")
		return
	}
	if p.AuthFile != "" && (!filepath.IsAbs(p.AuthFile) || strings.ContainsAny(p.AuthFile, "\x00\r\n") || len(p.AuthFile) > 4096) {
		fail(w, 400, "Authorization file must be an absolute path on the Apptrail server")
		return
	}
	if err := validEndpoint(p.Endpoint); err != nil {
		fail(w, 400, "Use a unix:///absolute/socket path or an HTTP(S) API base URL without credentials, query parameters, or fragments")
		return
	}
	pid := r.PathValue("id")
	if pid == "" {
		pid = id()
		if _, err := s.db.Exec("INSERT INTO providers(id,name,endpoint,enabled,kind,auth_file) VALUES(?,?,?,?,?,?)", pid, p.Name, p.Endpoint, p.Enabled, p.Type, p.AuthFile); err != nil {
			dbError(w, err)
			return
		}
	} else {
		var kind string
		if err := s.db.QueryRow("SELECT kind FROM providers WHERE id=?", pid).Scan(&kind); err != nil {
			if err == sql.ErrNoRows {
				notFound(w)
			} else {
				dbError(w, err)
			}
			return
		}
		if p.Type != kind {
			fail(w, 400, "Provider type cannot be changed; create a new provider instead")
			return
		}
		res, err := s.db.Exec("UPDATE providers SET name=?,endpoint=?,enabled=?,auth_file=? WHERE id=?", p.Name, p.Endpoint, p.Enabled, p.AuthFile, pid)
		if !changed(w, res, err) {
			return
		}
	}
	reply(w, map[string]string{"id": pid})
}
func (s *server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	if !s.scans.TryLock() {
		fail(w, 409, "Wait for the active scan to finish")
		return
	}
	defer s.scans.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM providers WHERE id=?", r.PathValue("id"))
	if !changed(w, res, err) {
		return
	}
	if _, err = tx.Exec("UPDATE observations SET present=0 WHERE provider=?", r.PathValue("id")); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}
func (s *server) scanProviderHTTP(w http.ResponseWriter, r *http.Request) {
	ps, err := s.providers()
	if err != nil {
		dbError(w, err)
		return
	}
	for _, p := range ps {
		if p.ID == r.PathValue("id") {
			if !p.Enabled {
				fail(w, 400, "Enable this provider before scanning")
				return
			}
			if err = s.scan(r.Context(), p); err != nil {
				fail(w, 502, err.Error())
				return
			}
			reply(w, map[string]bool{"ok": true})
			return
		}
	}
	notFound(w)
}
func (s *server) scan(ctx context.Context, p provider) error {
	if !s.scans.TryLock() {
		return fmt.Errorf("another scan is already running")
	}
	defer s.scans.Unlock()
	if err := s.db.QueryRow("SELECT name,endpoint,enabled,kind,auth_file FROM providers WHERE id=?", p.ID).Scan(&p.Name, &p.Endpoint, &p.Enabled, &p.Type, &p.AuthFile); err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var obs []observation
	var warning string
	var err error
	switch p.Type {
	case "docker":
		obs, warning, err = dockerSnapshot(ctx, p)
	case "caddy":
		obs, warning, err = caddySnapshot(ctx, p)
	case "traefik":
		obs, warning, err = traefikSnapshot(ctx, p)
	default:
		err = fmt.Errorf("unsupported provider type %q", p.Type)
	}
	if err == nil {
		err = s.reconcile(p.ID, obs)
	}
	if err != nil {
		if _, e := s.db.Exec("UPDATE providers SET error=? WHERE id=?", err.Error(), p.ID); e != nil {
			log.Print(e)
		}
		return err
	}
	_, err = s.db.Exec("UPDATE providers SET scanned=?,error=?,count=? WHERE id=?", time.Now().Unix(), warning, len(obs), p.ID)
	return err
}
func (s *server) scheduler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		ps, err := s.providers()
		if err != nil {
			log.Print(err)
		} else {
			for _, p := range ps {
				if p.Enabled {
					if err = s.scan(ctx, p); err != nil {
						log.Printf("Provider %s: %v", p.Name, err)
					}
				}
			}
		}
		apps, err := s.apps()
		if err == nil {
			var workers sync.WaitGroup
			slots := make(chan struct{}, 4)
			for _, a := range apps {
				if ctx.Err() != nil {
					break
				}
				if a.URL != "" && a.Lifecycle == "present" {
					slots <- struct{}{}
					workers.Go(func() {
						defer func() { <-slots }()
						if err := s.enrich(ctx, a.ID); err != nil && ctx.Err() == nil {
							log.Printf("Metadata %s: %v", a.ID, err)
						}
					})
				}
			}
			workers.Wait()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type container struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
	Labels map[string]string
}

func dockerSnapshot(ctx context.Context, p provider) ([]observation, string, error) {
	client, err := newProviderClient(p)
	if err != nil {
		return nil, "", err
	}
	defer client.Close()
	// ponytail: Docker API v1.44 (Engine 25+); negotiate /version if older daemon support is needed.
	var containers []container
	if err = client.Get(ctx, "/v1.44/containers/json?all=true", &containers); err != nil {
		return nil, "", err
	}
	if containers == nil {
		return nil, "", fmt.Errorf("Invalid Docker response: expected a container array")
	}
	out := []observation{}
	unresolved := 0
	for _, c := range containers {
		if c.ID == "" {
			return nil, "", fmt.Errorf("Invalid Docker response: container ID missing")
		}
		l := c.Labels
		if strings.EqualFold(l["apptrail.enable"], "false") {
			continue
		}
		name := strings.TrimPrefix(first(c.Names), "/")
		if name == "" {
			name = c.ID
		}
		native := name
		if project, service := l["com.docker.compose.project"], l["com.docker.compose.service"]; project != "" && service != "" {
			native = project + "/" + service
		}
		routes := map[string]string{}
		if raw := l["apptrail.url"]; raw != "" {
			if _, err := validURL(raw); err == nil {
				routes["explicit"] = raw
			} else {
				unresolved++
			}
		}
		if len(routes) == 0 {
			for key, rule := range l {
				if !strings.HasPrefix(key, "traefik.http.routers.") || !strings.HasSuffix(key, ".rule") {
					continue
				}
				router := strings.TrimSuffix(strings.TrimPrefix(key, "traefik.http.routers."), ".rule")
				matches, err := traefikMatches(rule)
				if err != nil {
					unresolved++
					continue
				}
				scheme := "http"
				prefix := "traefik.http.routers." + router + "."
				for k, v := range l {
					if strings.HasPrefix(k, prefix+"tls.") || (k == prefix+"tls" && v != "false") {
						scheme = "https"
					}
				}
				port := 80
				if scheme == "https" {
					port = 443
				}
				for _, m := range matches {
					if raw, ok := launchURL(scheme, m, port); ok {
						routes[router+":"+m.Host+":"+m.Path] = raw
					} else {
						unresolved++
					}
				}
			}
		}
		if len(routes) == 0 {
			routes["unresolved"] = ""
		}
		keys := make([]string, 0, len(routes))
		for k := range routes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, route := range keys {
			raw := routes[route]
			source := "compose:" + native + "/router:" + route
			o := observation{Source: source, Keys: []string{"native:" + p.ID + ":" + source}, Facts: map[string]fact{"name": {name, 20, p.Name + " · container name"}, "health": {"unknown", 20, p.Name + " · Docker"}}}
			if l["apptrail.id"] != "" {
				o.Keys = append([]string{"explicit:" + l["apptrail.id"]}, o.Keys...)
			}
			if raw != "" {
				o.Keys = append(o.Keys, "url:"+normalizedURL(raw))
				o.Facts["url"] = fact{raw, 30, p.Name + " · Traefik route"}
			}
			if c.State != "running" {
				o.Facts["health"] = fact{"unreachable", 60, p.Name + " · Docker state"}
			} else if strings.Contains(c.Status, "(unhealthy)") {
				o.Facts["health"] = fact{"unhealthy", 60, p.Name + " · Docker health"}
			} else if strings.Contains(c.Status, "(healthy)") {
				o.Facts["health"] = fact{"healthy", 60, p.Name + " · Docker health"}
			}
			for _, k := range []string{"name", "description", "category", "icon", "url", "manifest"} {
				if v := l["apptrail."+k]; v != "" {
					if k == "url" || k == "icon" {
						if _, err := validURL(v); err != nil {
							continue
						}
					}
					o.Facts[k] = fact{v, 80, p.Name + " · apptrail." + k}
				}
			}
			for key, label := range map[string]string{"health_url": "apptrail.health.url", "health_method": "apptrail.health.method"} {
				if value := l[label]; value != "" {
					o.Facts[key] = fact{value, 80, p.Name + " · " + label}
				}
			}
			out = append(out, o)
		}
	}
	warning := ""
	if unresolved > 0 {
		warning = fmt.Sprintf("%d route(s) could not be resolved. Add apptrail.url or use literal Host, Path, and PathPrefix matchers.", unresolved)
	}
	return out, warning, nil
}
func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}
