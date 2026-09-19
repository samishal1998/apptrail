package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const maxProxyRoutes = 10000

type routeMatch struct {
	Host, Path string
	Unknown    bool
}

func pathStart(pattern string) (string, bool) {
	if pattern == "" || pattern == "*" {
		return "/", true
	}
	path := strings.TrimSuffix(pattern, "*")
	return path, strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "*{}?#\r\n\x00")
}
func pathMatches(pattern, path string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == path
}
func combineMatches(a, b []routeMatch) ([]routeMatch, error) {
	out := []routeMatch{}
	for _, left := range a {
		for _, right := range b {
			m := left
			m.Unknown = m.Unknown || right.Unknown
			if right.Host != "" {
				if m.Host != "" && !strings.EqualFold(m.Host, right.Host) {
					if strings.ContainsAny(m.Host+right.Host, "*{}") {
						m.Unknown = true
					} else {
						continue
					}
				}
				m.Host = right.Host
			}
			if right.Path != "" {
				if m.Path == "" || m.Path == "*" {
					m.Path = right.Path
				} else {
					lp, lok := pathStart(m.Path)
					rp, rok := pathStart(right.Path)
					if !lok || !rok {
						m.Unknown = true
					} else if !strings.HasSuffix(m.Path, "*") {
						if !pathMatches(right.Path, lp) {
							continue
						}
					} else if !strings.HasSuffix(right.Path, "*") {
						if !pathMatches(m.Path, rp) {
							continue
						}
						m.Path = right.Path
					} else if strings.HasPrefix(rp, lp) {
						m.Path = right.Path
					} else if !strings.HasPrefix(lp, rp) {
						continue
					}
				}
			}
			out = append(out, m)
			if len(out) > maxProxyRoutes {
				return nil, fmt.Errorf("route expansion exceeds %d entries", maxProxyRoutes)
			}
		}
	}
	return out, nil
}

func traefikMatches(rule string) ([]routeMatch, error) {
	if len(rule) > 65536 {
		return nil, fmt.Errorf("rule exceeds 64 KiB")
	}
	expr, err := parser.ParseExpr(rule)
	if err != nil {
		return nil, fmt.Errorf("invalid Traefik rule")
	}
	var visit func(ast.Expr, int) ([]routeMatch, error)
	visit = func(expr ast.Expr, depth int) ([]routeMatch, error) {
		if depth > 32 {
			return nil, fmt.Errorf("rule nesting exceeds 32 levels")
		}
		switch e := expr.(type) {
		case *ast.ParenExpr:
			return visit(e.X, depth+1)
		case *ast.BinaryExpr:
			left, err := visit(e.X, depth+1)
			if err != nil {
				return nil, err
			}
			right, err := visit(e.Y, depth+1)
			if err != nil {
				return nil, err
			}
			if e.Op == token.LAND {
				return combineMatches(left, right)
			}
			if e.Op == token.LOR {
				if len(left)+len(right) > maxProxyRoutes {
					return nil, fmt.Errorf("too many rule alternatives")
				}
				return append(left, right...), nil
			}
		case *ast.CallExpr:
			name, ok := e.Fun.(*ast.Ident)
			if !ok || len(e.Args) == 0 || len(e.Args) > maxProxyRoutes {
				return nil, fmt.Errorf("unsupported matcher or too many arguments")
			}
			// ponytail: literal Host/Path/PathPrefix only; add explicit handling for other matchers rather than guessing URLs.
			if name.Name != "Host" && name.Name != "Path" && name.Name != "PathPrefix" {
				return nil, fmt.Errorf("unsupported matcher %s", name.Name)
			}
			out := []routeMatch{}
			for _, arg := range e.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return nil, fmt.Errorf("matcher arguments must be literal strings")
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				m := routeMatch{}
				if name.Name == "Host" {
					m.Host = strings.ToLower(value)
				} else {
					m.Path = value
					if strings.ContainsAny(value, "*{}?#\r\n\x00") {
						return nil, fmt.Errorf("dynamic paths are unsupported")
					}
					if name.Name == "PathPrefix" {
						m.Path += "*"
					}
				}
				out = append(out, m)
			}
			return out, nil
		}
		return nil, fmt.Errorf("unsupported rule expression")
	}
	out, err := visit(expr, 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func listenerPort(address string) (int, bool) {
	// Traefik entrypoints may explicitly select the TCP protocol, e.g. :443/tcp.
	if len(address) >= 4 && strings.EqualFold(address[len(address)-4:], "/tcp") {
		address = address[:len(address)-4]
	}
	for _, prefix := range []string{"tcp/", "tcp4/", "tcp6/"} {
		address = strings.TrimPrefix(address, prefix)
	}
	if strings.Contains(address, "/") {
		return 0, false
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(port)
	return n, err == nil && n > 0 && n <= 65535
}
func launchURL(scheme string, m routeMatch, port int) (string, bool) {
	path, ok := pathStart(m.Path)
	if !ok || m.Unknown || m.Host == "" || port < 1 || port > 65535 {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(m.Host), ".")
	if strings.ContainsAny(host, "*{} /\\?#@\t\r\n\x00") {
		return "", false
	}
	if strings.Contains(host, ":") {
		ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
		if err != nil {
			return "", false
		}
		host = ip.String()
	}
	authority := host
	if (scheme == "http" && port != 80) || (scheme == "https" && port != 443) {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	} else if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	u := &url.URL{Scheme: scheme, Host: authority, Path: path}
	if _, err := validURL(u.String()); err != nil {
		return "", false
	}
	return u.String(), true
}
func proxyObservation(p provider, source, name, raw, warning string) observation {
	o := observation{Source: source, Keys: []string{"native:" + p.ID + ":" + source}, Facts: map[string]fact{"name": {name, 15, p.Name + " · route"}, "health": {"unknown", 20, p.Name + " · discovery"}}}
	if raw != "" {
		o.Keys = append(o.Keys, "url:"+normalizedURL(raw))
		o.Facts["url"] = fact{raw, 35, p.Name + " · " + p.Type + " API"}
	}
	if warning != "" {
		o.Facts["discovery_warning"] = fact{warning, 20, p.Name + " · route diagnostics"}
	}
	return o
}
func routeWarning(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d route(s) could not be fully resolved. Inspect Discover/source details and set a launch URL override for dynamic matchers or externally mapped ports.", count)
}
