package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var errProbeBlocked = errors.New("probe blocked by network policy; private-network probing is configurable in Settings")
var probeSlots = make(chan struct{}, 8)

func acquireProbe(ctx context.Context) error {
	select {
	case probeSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func allowedIP(ip netip.Addr, private bool) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// Block special-use/reserved networks even when private-network discovery is enabled.
	for _, cidr := range []string{"0.0.0.0/8", "100.100.100.200/32", "168.63.129.16/32", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "fd00:ec2::254/128", "2001::/32", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	if ip.IsPrivate() || ip.IsLoopback() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return private
	}
	return ip.IsGlobalUnicast()
}
func probeClient(private bool) *http.Client {
	t := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("hostname has no addresses")
		}
		for _, ip := range ips {
			if !allowedIP(ip, private) {
				return nil, errProbeBlocked
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: t, Timeout: 6 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return fmt.Errorf("redirect limit reached")
		}
		_, err := validURL(r.URL.String())
		return err
	}}
}
func fetch(ctx context.Context, c *http.Client, raw string) ([]byte, *http.Response, error) {
	return fetchMethod(ctx, c, raw, "GET")
}
func fetchMethod(ctx context.Context, c *http.Client, raw, method string) ([]byte, *http.Response, error) {
	if method != "GET" && method != "HEAD" {
		return nil, nil, fmt.Errorf("Only GET and HEAD probes are supported")
	}
	if _, err := validURL(raw); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Apptrail/0.1 (+metadata discovery)")
	res, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, res, err
	}
	if len(b) > 1<<20 {
		return nil, res, fmt.Errorf("response exceeds 1 MiB")
	}
	return b, res, nil
}
func sameOriginURL(base *url.URL, raw string) string {
	r, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u := base.ResolveReference(r)
	if _, err = validURL(u.String()); err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
		return ""
	}
	return u.String()
}
func metadata(b []byte, base *url.URL) map[string]string {
	out := map[string]string{}
	doc, err := html.Parse(strings.NewReader(string(b)))
	if err != nil {
		return out
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			switch n.Data {
			case "title":
				if n.FirstChild != nil && out["name"] == "" {
					out["name"] = strings.TrimSpace(n.FirstChild.Data)
				}
			case "meta":
				switch strings.ToLower(attrs["name"] + attrs["property"]) {
				case "description", "og:description":
					if out["description"] == "" {
						out["description"] = attrs["content"]
					}
				case "og:title":
					if out["name"] == "" {
						out["name"] = attrs["content"]
					}
				}
			case "link":
				rel := " " + strings.ToLower(attrs["rel"]) + " "
				if strings.Contains(rel, " icon ") {
					out["icon"] = sameOriginURL(base, attrs["href"])
				}
				if strings.Contains(rel, " manifest ") {
					out["webmanifest"] = sameOriginURL(base, attrs["href"])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return out
}
func (s *server) enrich(parent context.Context, appID string) error {
	a, err := s.app(appID)
	if err != nil {
		return err
	}
	if a.URL == "" {
		return fmt.Errorf("No launch URL; add apptrail.url or a URL override")
	}
	var private bool
	if err = s.db.QueryRow("SELECT private_probes FROM settings WHERE id=1").Scan(&private); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 18*time.Second)
	defer cancel()
	if err := acquireProbe(ctx); err != nil {
		return err
	}
	defer func() { <-probeSlots }()
	client := probeClient(private)
	defer client.CloseIdleConnections()
	o := observation{Source: appID, Facts: map[string]fact{}}
	data, res, probeErr := fetch(ctx, client, a.URL)
	if probeErr != nil {
		// Preserve the last successful metadata during outages; only health and diagnostics change.
		var raw string
		err := s.db.QueryRow("SELECT payload FROM observations WHERE provider='http' AND app_id=?", appID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if err = json.Unmarshal([]byte(raw), &o); err != nil {
				return err
			}
		}
		state := "unreachable"
		if errors.Is(probeErr, errProbeBlocked) {
			state = "unknown"
		}
		o.Facts["health"] = fact{state, 40, "HTTP probe"}
		o.Facts["probe_error"] = fact{probeErr.Error(), 40, "HTTP probe"}
	} else {
		health := "healthy"
		if res.StatusCode >= 400 {
			health = "degraded"
		}
		if res.StatusCode >= 500 {
			health = "unhealthy"
		}
		o.Facts["health"] = fact{health, 40, "HTTP status " + res.Status}
		base := res.Request.URL
		if res.StatusCode >= 200 && res.StatusCode < 300 && strings.Contains(res.Header.Get("Content-Type"), "text/html") {
			meta := metadata(data, base)
			for k, v := range meta {
				if k != "webmanifest" && v != "" && len(v) <= 2000 {
					o.Facts[k] = fact{v, 50, "HTTP metadata"}
				}
			}
			if o.Facts["icon"].Value == "" && meta["webmanifest"] != "" {
				if b, response, e := fetch(ctx, client, meta["webmanifest"]); e == nil && response.StatusCode == 200 && sameOriginURL(base, response.Request.URL.String()) != "" {
					var wm struct{ Icons []struct{ Src string } }
					if json.Unmarshal(b, &wm) == nil {
						for _, i := range wm.Icons {
							if icon := sameOriginURL(response.Request.URL, i.Src); icon != "" {
								o.Facts["icon"] = fact{icon, 45, "Web app manifest"}
								break
							}
						}
					}
				}
			}
		}
		if o.Facts["icon"].Value == "" {
			o.Facts["icon"] = fact{base.Scheme + "://" + base.Host + "/favicon.ico", 10, "Favicon fallback"}
		}
		manifestURL := sameOriginURL(base, "/.well-known/apptrail.json")
		if v := a.Fields["manifest"].Value; v != "" {
			manifestURL = sameOriginURL(base, v)
		}
		if manifestURL != "" {
			if b, response, e := fetch(ctx, client, manifestURL); e == nil && response.StatusCode == 200 && sameOriginURL(base, response.Request.URL.String()) != "" {
				var m struct {
					Version     string `json:"version"`
					ID          string `json:"id"`
					Name        string `json:"name"`
					Description string `json:"description"`
					Icon        string `json:"icon"`
					Category    string `json:"category"`
					Health      *struct {
						URL      string `json:"url"`
						Method   string `json:"method"`
						Expected []int  `json:"expected_status"`
					} `json:"health"`
				}
				if json.Unmarshal(b, &m) == nil && m.Version == "1" {
					if required(m.ID, 200) {
						o.Facts["manifest_id"] = fact{m.ID, 90, "Apptrail manifest"}
					}
					for k, v := range map[string]string{"name": m.Name, "description": m.Description, "category": m.Category, "icon": sameOriginURL(base, m.Icon)} {
						if v != "" && len(v) <= 2000 && (k != "icon" || m.Icon != "") {
							o.Facts[k] = fact{v, 90, "Apptrail manifest"}
						}
					}
					if m.Health != nil && m.Health.URL != "" && (m.Health.Method == "" || m.Health.Method == "GET" || m.Health.Method == "HEAD") {
						if healthURL := sameOriginURL(base, m.Health.URL); healthURL != "" {
							method := m.Health.Method
							if method == "" {
								method = "GET"
							}
							_, response, e := fetchMethod(ctx, client, healthURL, method)
							state := "unreachable"
							if e == nil {
								state = "unhealthy"
								expected := m.Health.Expected
								if len(expected) == 0 {
									expected = []int{200}
								}
								for _, status := range expected {
									if response.StatusCode == status {
										state = "healthy"
									}
								}
							}
							o.Facts["health"] = fact{state, 90, "Apptrail manifest health"}
						}
					}
				}
			}
		}
		if raw := a.Fields["health_url"].Value; raw != "" && o.Facts["health"].Priority < 80 {
			if target := sameOriginURL(base, raw); target != "" {
				method := a.Fields["health_method"].Value
				if method == "" {
					method = "GET"
				}
				_, response, err := fetchMethod(ctx, client, target, method)
				state := "unreachable"
				if err == nil {
					state = "unhealthy"
					if response.StatusCode >= 200 && response.StatusCode < 300 {
						state = "healthy"
					}
				}
				o.Facts["health"] = fact{state, 80, "Apptrail health label"}
			}
		}
	}
	// Recheck the app's URL before storing a slow probe result so an edited URL cannot receive stale metadata.
	current, err := s.app(appID)
	if err != nil {
		return err
	}
	if current.URL != a.URL {
		return fmt.Errorf("Launch URL changed during probe; refresh again")
	}
	if manifestID := o.Facts["manifest_id"].Value; manifestID != "" {
		var explicit int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM identities WHERE app_id=? AND key LIKE 'explicit:%'", appID).Scan(&explicit); err != nil {
			return err
		}
		if explicit == 0 {
			var existing string
			err := s.db.QueryRow("SELECT app_id FROM identities WHERE key=?", "explicit:"+manifestID).Scan(&existing)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if existing != "" && existing != appID {
				o.Facts["probe_error"] = fact{"Manifest identity belongs to another app; inspect the Apptrail IDs before merging", 90, "Apptrail manifest"}
			} else {
				if _, err = s.db.Exec("INSERT OR IGNORE INTO identities(key,app_id) VALUES(?,?)", "explicit:"+manifestID, appID); err != nil {
					return err
				}
			}
		}
	}
	_, err = s.db.Exec("INSERT INTO observations(provider,source,app_id,payload,present,seen) VALUES('http',?,?,?,1,?) ON CONFLICT(provider,source) DO UPDATE SET payload=excluded.payload,seen=excluded.seen", appID, appID, encode(o), time.Now().Unix())
	if err != nil {
		return err
	}
	return probeErr
}
func (s *server) refreshApp(w http.ResponseWriter, r *http.Request) {
	if err := s.enrich(r.Context(), r.PathValue("id")); err != nil {
		if err == sql.ErrNoRows {
			notFound(w)
		} else {
			fail(w, 422, err.Error())
		}
		return
	}
	a, err := s.app(r.PathValue("id"))
	if err != nil {
		dbError(w, err)
		return
	}
	reply(w, a)
}
func (s *server) icon(w http.ResponseWriter, r *http.Request) {
	a, err := s.app(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	if !s.authed(r) && (a.Hidden || !s.isPublicApp(a.ID)) {
		notFound(w)
		return
	}
	if a.Icon == "" {
		notFound(w)
		return
	}
	var private bool
	if err = s.db.QueryRow("SELECT private_probes FROM settings WHERE id=1").Scan(&private); err != nil {
		dbError(w, err)
		return
	}
	c := probeClient(private)
	defer c.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err = acquireProbe(ctx); err != nil {
		fail(w, 503, "Icon service is busy")
		return
	}
	defer func() { <-probeSlots }()
	b, res, err := fetch(ctx, c, a.Icon)
	if err != nil || res.StatusCode != 200 {
		notFound(w)
		return
	}
	t := strings.Split(res.Header.Get("Content-Type"), ";")[0]
	switch t {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/vnd.microsoft.icon", "image/svg+xml":
	default:
		notFound(w)
		return
	}
	w.Header().Set("Content-Type", t)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(b)
}
