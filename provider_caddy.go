package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type caddyRoute struct {
	ID     string                       `json:"@id"`
	Match  []map[string]json.RawMessage `json:"match"`
	Handle []caddyHandler               `json:"handle"`
}
type caddyHandler struct {
	ID        string       `json:"@id"`
	Handler   string       `json:"handler"`
	Routes    []caddyRoute `json:"routes"`
	Upstreams []struct {
		Dial string `json:"dial"`
	} `json:"upstreams"`
}
type caddyServer struct {
	Listen    []string          `json:"listen"`
	Routes    []caddyRoute      `json:"routes"`
	TLS       []json.RawMessage `json:"tls_connection_policies"`
	AutoHTTPS struct {
		Disable bool     `json:"disable"`
		Skip    []string `json:"skip"`
	} `json:"automatic_https"`
}
type caddyHTTP struct {
	HTTPPort  int                    `json:"http_port"`
	HTTPSPort int                    `json:"https_port"`
	Servers   map[string]caddyServer `json:"servers"`
}

func caddyMatches(in []routeMatch, sets []map[string]json.RawMessage) ([]routeMatch, error) {
	if len(sets) == 0 {
		return in, nil
	}
	choices := []routeMatch{}
	for _, set := range sets {
		var hosts, paths []string
		unknown := false
		for key, raw := range set {
			switch key {
			case "host":
				if err := json.Unmarshal(raw, &hosts); err != nil {
					return nil, fmt.Errorf("invalid Caddy host matcher")
				}
			case "path":
				if err := json.Unmarshal(raw, &paths); err != nil {
					return nil, fmt.Errorf("invalid Caddy path matcher")
				}
			default:
				unknown = true
			}
		}
		if (set["host"] != nil && len(hosts) == 0) || (set["path"] != nil && len(paths) == 0) {
			continue
		}
		if len(hosts) == 0 {
			hosts = []string{""}
		}
		if len(paths) == 0 {
			paths = []string{""}
		}
		for _, host := range hosts {
			for _, path := range paths {
				choices = append(choices, routeMatch{Host: strings.ToLower(host), Path: path, Unknown: unknown})
				if len(choices) > maxProxyRoutes {
					return nil, fmt.Errorf("Caddy matcher expansion exceeds %d routes", maxProxyRoutes)
				}
			}
		}
	}
	return combineMatches(in, choices)
}
func caddyUsesTLS(s caddyServer, httpPort, httpsPort int) bool {
	if len(s.TLS) > 0 {
		return true
	}
	if s.AutoHTTPS.Disable {
		return false
	}
	nonHTTP, onlyHTTPS := false, len(s.Listen) > 0
	for _, addr := range s.Listen {
		port, ok := listenerPort(addr)
		if !ok {
			onlyHTTPS = false
			continue
		}
		if port != httpPort {
			nonHTTP = true
		}
		if port != httpsPort {
			onlyHTTPS = false
		}
	}
	if !nonHTTP {
		return false
	}
	if onlyHTTPS {
		return true
	}
	for _, route := range s.Routes {
		for _, set := range route.Match {
			var hosts []string
			if json.Unmarshal(set["host"], &hosts) == nil {
				for _, host := range hosts {
					if host != "" && !contains(s.AutoHTTPS.Skip, host) {
						return true
					}
				}
			}
		}
	}
	return false
}

func caddySnapshot(ctx context.Context, p provider) ([]observation, string, error) {
	c, err := newProviderClient(p)
	if err != nil {
		return nil, "", err
	}
	defer c.Close()
	var root map[string]json.RawMessage
	if err = c.Get(ctx, "/config/", &root); err != nil {
		return nil, "", err
	}
	if root == nil {
		return nil, "", fmt.Errorf("invalid Caddy response: expected a configuration object")
	}
	if len(root) > 0 && root["apps"] == nil && root["admin"] == nil && root["logging"] == nil && root["storage"] == nil {
		return nil, "", fmt.Errorf("response does not look like a Caddy configuration")
	}
	var apps map[string]json.RawMessage
	if raw := root["apps"]; raw != nil {
		if err = json.Unmarshal(raw, &apps); err != nil {
			return nil, "", fmt.Errorf("invalid Caddy apps configuration")
		}
	}
	if apps["http"] == nil {
		return []observation{}, "", nil
	}
	var config caddyHTTP
	if err = json.Unmarshal(apps["http"], &config); err != nil {
		return nil, "", fmt.Errorf("invalid Caddy HTTP configuration")
	}
	if config.HTTPPort == 0 {
		config.HTTPPort = 80
	}
	if config.HTTPSPort == 0 {
		config.HTTPSPort = 443
	}
	names := make([]string, 0, len(config.Servers))
	for name := range config.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []observation{}
	warnings := 0
	seen := map[string]bool{}
	for _, name := range names {
		srv := config.Servers[name]
		tls := caddyUsesTLS(srv, config.HTTPPort, config.HTTPSPort)
		var walk func([]caddyRoute, []routeMatch, int) error
		walk = func(routes []caddyRoute, inherited []routeMatch, depth int) error {
			if depth > 32 {
				return fmt.Errorf("Caddy route nesting exceeds 32 levels")
			}
			for _, route := range routes {
				matches, err := caddyMatches(inherited, route.Match)
				if err != nil {
					return err
				}
				for _, handler := range route.Handle {
					if handler.Handler == "subroute" {
						if err = walk(handler.Routes, matches, depth+1); err != nil {
							return err
						}
						continue
					}
					if handler.Handler != "reverse_proxy" && handler.Handler != "file_server" {
						continue
					}
					urls := map[string]bool{}
					unresolved := false
					for _, match := range matches {
						if len(srv.Listen) == 0 {
							unresolved = true
						}
						for _, listen := range srv.Listen {
							port, ok := listenerPort(listen)
							if !ok {
								unresolved = true
								continue
							}
							scheme := "http"
							if tls && port != config.HTTPPort {
								scheme = "https"
							}
							if raw, ok := launchURL(scheme, match, port); ok {
								urls[raw] = true
							} else {
								unresolved = true
							}
						}
					}
					keys := make([]string, 0, len(urls))
					for raw := range urls {
						keys = append(keys, raw)
					}
					sort.Strings(keys)
					routeID := handler.ID
					if routeID == "" {
						routeID = route.ID
					}
					for _, raw := range keys {
						source := "server:" + name + "/url:" + normalizedURL(raw)
						if seen[source] {
							continue
						}
						seen[source] = true
						o := proxyObservation(p, source, hostnameOf(raw), raw, "")
						if routeID != "" && len(keys) == 1 {
							o.Keys = append(o.Keys, "native:"+p.ID+":caddy-id:"+routeID)
						}
						o.Facts["server"] = fact{name, 20, p.Name + " · Caddy server"}
						o.Facts["handler"] = fact{handler.Handler, 20, p.Name + " · Caddy handler"}
						if len(handler.Upstreams) == 1 {
							o.Facts["backend"] = fact{handler.Upstreams[0].Dial, 20, p.Name + " · Caddy upstream"}
						}
						out = append(out, o)
					}
					if unresolved {
						warnings++
						// ponytail: unnamed unresolved handlers use configuration fingerprints; explicit @id gives them stable identity across edits.
						if routeID == "" {
							routeID = tokenHash(encode(handler) + encode(matches))[:24]
						}
						source := "server:" + name + "/unresolved:" + routeID
						if !seen[source] {
							seen[source] = true
							display := name + " route"
							if len(matches) == 1 && matches[0].Host != "" {
								display = matches[0].Host
							}
							o := proxyObservation(p, source, display, "", "A concrete host, supported host/path matchers, and a single-port TCP listener are required")
							o.Facts["server"] = fact{name, 20, p.Name + " · Caddy server"}
							o.Facts["handler"] = fact{handler.Handler, 20, p.Name + " · Caddy handler"}
							if len(handler.Upstreams) == 1 {
								o.Facts["backend"] = fact{handler.Upstreams[0].Dial, 20, p.Name + " · Caddy upstream"}
							}
							out = append(out, o)
						}
					}
					if len(out) > maxProxyRoutes {
						return fmt.Errorf("Caddy discovery exceeds %d routes", maxProxyRoutes)
					}
				}
			}
			return nil
		}
		if err = walk(srv.Routes, []routeMatch{{}}, 0); err != nil {
			return nil, "", err
		}
	}
	return out, routeWarning(warnings), nil
}
func hostnameOf(raw string) string { u, _ := validURL(raw); return u.Hostname() }
