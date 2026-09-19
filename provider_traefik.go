package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type traefikRouter struct {
	Name        string          `json:"name"`
	Rule        string          `json:"rule"`
	Service     string          `json:"service"`
	Status      string          `json:"status"`
	Provider    string          `json:"provider"`
	EntryPoints []string        `json:"entryPoints"`
	Using       []string        `json:"using"`
	TLS         json.RawMessage `json:"tls"`
}
type traefikEntryPoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	HTTP    struct {
		TLS json.RawMessage `json:"tls"`
	} `json:"http"`
}
type traefikService struct {
	Name         string            `json:"name"`
	Status       string            `json:"status"`
	ServerStatus map[string]string `json:"serverStatus"`
	LoadBalancer *struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	} `json:"loadBalancer"`
}

func hasTLS(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != "false"
}

func traefikSnapshot(ctx context.Context, p provider) ([]observation, string, error) {
	c, err := newProviderClient(p)
	if err != nil {
		return nil, "", err
	}
	defer c.Close()
	var routers []traefikRouter
	var entries []traefikEntryPoint
	var services []traefikService
	// ponytail: one bounded page per endpoint (10,000 records); add pagination for larger installations.
	query := fmt.Sprintf("?per_page=%d", maxProxyRoutes+1)
	if err = c.Get(ctx, "/api/http/routers"+query, &routers); err != nil {
		return nil, "", err
	}
	if err = c.Get(ctx, "/api/entrypoints"+query, &entries); err != nil {
		return nil, "", err
	}
	if err = c.Get(ctx, "/api/http/services"+query, &services); err != nil {
		return nil, "", err
	}
	if routers == nil || entries == nil || services == nil {
		return nil, "", fmt.Errorf("invalid Traefik response: expected router, entrypoint, and service arrays")
	}
	if len(routers) > maxProxyRoutes || len(entries) > maxProxyRoutes || len(services) > maxProxyRoutes {
		return nil, "", fmt.Errorf("Traefik API snapshot exceeds %d records", maxProxyRoutes)
	}
	byEntry := map[string]traefikEntryPoint{}
	for _, e := range entries {
		if e.Name == "" {
			return nil, "", fmt.Errorf("Traefik entrypoint name is missing")
		}
		byEntry[e.Name] = e
	}
	byService := map[string]traefikService{}
	for _, s := range services {
		if s.Name == "" {
			return nil, "", fmt.Errorf("Traefik service name is missing")
		}
		byService[s.Name] = s
	}
	sort.Slice(routers, func(i, j int) bool { return routers[i].Name < routers[j].Name })
	out := []observation{}
	warnings := 0
	seen := map[string]bool{}
	for _, r := range routers {
		if r.Provider == "internal" || strings.HasSuffix(r.Name, "@internal") {
			continue
		}
		if r.Name == "" || r.Rule == "" {
			return nil, "", fmt.Errorf("Traefik router name or rule is missing")
		}
		if r.Status != "" && r.Status != "enabled" {
			warnings++
			continue
		}
		if r.Service == "noop@internal" {
			continue
		}
		name := strings.Split(r.Name, "@")[0]
		baseSource := "router:" + r.Name
		unresolved := func(reason string) {
			source := baseSource + "/unresolved"
			if seen[source] {
				return
			}
			seen[source] = true
			warnings++
			o := proxyObservation(p, source, name, "", reason)
			o.Facts["router"] = fact{r.Name, 20, p.Name + " · Traefik router"}
			o.Facts["service"] = fact{r.Service, 20, p.Name + " · Traefik service"}
			out = append(out, o)
		}
		matches, e := traefikMatches(r.Rule)
		if e != nil {
			unresolved(e.Error())
			continue
		}
		eps := r.Using
		if len(eps) == 0 {
			eps = r.EntryPoints
		}
		if len(eps) == 0 {
			unresolved("Router has no resolved entrypoints")
			continue
		}
		for _, entry := range eps {
			ep, ok := byEntry[entry]
			if !ok {
				unresolved("Entrypoint " + entry + " was not returned by the API")
				continue
			}
			port, ok := listenerPort(ep.Address)
			if !ok {
				unresolved("Entrypoint address is not a single TCP port: " + ep.Address)
				continue
			}
			scheme := "http"
			if hasTLS(r.TLS) || hasTLS(ep.HTTP.TLS) {
				scheme = "https"
			}
			for _, m := range matches {
				raw, ok := launchURL(scheme, m, port)
				if !ok {
					unresolved("A concrete hostname and literal path are required")
					continue
				}
				source := baseSource + "/entrypoint:" + entry + "/" + m.Host + "/" + m.Path
				if seen[source] {
					continue
				}
				seen[source] = true
				o := proxyObservation(p, source, name, raw, "")
				o.Facts["router"] = fact{r.Name, 20, p.Name + " · Traefik router"}
				o.Facts["entrypoint"] = fact{entry, 20, p.Name + " · Traefik entrypoint"}
				o.Facts["service"] = fact{r.Service, 20, p.Name + " · Traefik service"}
				service, found := byService[r.Service]
				if !found && !strings.Contains(r.Service, "@") {
					if _, suffix, ok := strings.Cut(r.Name, "@"); ok {
						service, found = byService[r.Service+"@"+suffix]
					}
				}
				if found {
					if service.LoadBalancer != nil && len(service.LoadBalancer.Servers) == 1 {
						o.Facts["backend"] = fact{service.LoadBalancer.Servers[0].URL, 20, p.Name + " · Traefik upstream"}
					}
					up, down := 0, 0
					for _, status := range service.ServerStatus {
						switch status {
						case "UP":
							up++
						case "DOWN":
							down++
						}
					}
					state := "unknown"
					if up > 0 {
						state = "healthy"
					}
					if down > 0 {
						state = "unhealthy"
						if up > 0 {
							state = "degraded"
						}
					}
					if service.Status != "" && service.Status != "enabled" {
						state = "unhealthy"
					}
					if state != "unknown" {
						o.Facts["health"] = fact{state, 55, p.Name + " · Traefik upstream status"}
					}
				}
				out = append(out, o)
				if len(out) > maxProxyRoutes {
					return nil, "", fmt.Errorf("Traefik discovery exceeds %d routes", maxProxyRoutes)
				}
			}
		}
	}
	if len(out) > maxProxyRoutes {
		return nil, "", fmt.Errorf("Traefik discovery exceeds %d routes", maxProxyRoutes)
	}
	return groupTraefikAliases(p, out), routeWarning(warnings), nil
}

func groupTraefikAliases(p provider, observations []observation) []observation {
	groups := map[string][]observation{}
	order := []string{}
	out := []observation{}
	for _, o := range observations {
		raw := o.Facts["url"].Value
		if raw == "" {
			out = append(out, o)
			continue
		}
		u, _ := validURL(normalizedURL(raw))
		source := "router:" + url.PathEscape(o.Facts["router"].Value) + "/path:" + url.PathEscape(u.EscapedPath())
		if _, ok := groups[source]; !ok {
			order = append(order, source)
		}
		groups[source] = append(groups[source], o)
	}
	for _, source := range order {
		entries := groups[source]
		// Prefer HTTPS, then keep the configured rule/entrypoint order.
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.HasPrefix(entries[i].Facts["url"].Value, "https:") && !strings.HasPrefix(entries[j].Facts["url"].Value, "https:")
		})
		o := entries[0]
		o.Source = source
		o.AliasGroup = true
		o.Keys = []string{"native:" + p.ID + ":" + source, "url:" + normalizedURL(o.Facts["url"].Value)}
		entrypoints := []string{}
		for _, entry := range entries {
			o.URLs = appendURL(o.URLs, entry.Facts["url"].Value)
			key := "url:" + normalizedURL(entry.Facts["url"].Value)
			if !contains(o.Keys, key) {
				o.Keys = append(o.Keys, key)
			}
			if ep := entry.Facts["entrypoint"].Value; !contains(entrypoints, ep) {
				entrypoints = append(entrypoints, ep)
			}
		}
		o.Facts["entrypoints"] = fact{strings.Join(entrypoints, ", "), 20, p.Name + " · Traefik entrypoints"}
		// ponytail: one router/path means one app; use separate routers when host-based virtual apps are genuinely distinct.
		out = append(out, o)
	}
	return out
}
