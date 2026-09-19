package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type providerClient struct {
	client                    *http.Client
	base, authorization, name string
}

func newProviderClient(p provider) (*providerClient, error) {
	if err := validEndpoint(p.Endpoint); err != nil {
		return nil, err
	}
	u, _ := url.Parse(p.Endpoint)
	c := &providerClient{base: strings.TrimRight(p.Endpoint, "/"), name: map[string]string{"docker": "Docker", "caddy": "Caddy", "traefik": "Traefik"}[p.Type]}
	if c.name == "" {
		c.name = "Docker"
	}
	if p.AuthFile != "" {
		info, err := os.Stat(p.AuthFile)
		if err != nil {
			return nil, fmt.Errorf("authorization file: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > 8192 {
			return nil, fmt.Errorf("authorization file must be a regular file of at most 8 KiB")
		}
		f, err := os.Open(p.AuthFile)
		if err != nil {
			return nil, fmt.Errorf("authorization file: %w", err)
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil {
			return nil, fmt.Errorf("could not read authorization file")
		}
		if len(b) > 8192 {
			return nil, fmt.Errorf("authorization file exceeds 8 KiB")
		}
		c.authorization = strings.TrimSpace(string(b))
		if c.authorization == "" {
			return nil, fmt.Errorf("authorization file is empty")
		}
		for _, v := range c.authorization {
			if v < 32 || v == 127 {
				return nil, fmt.Errorf("authorization file must contain one HTTP Authorization header value")
			}
		}
	}
	t := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	if u.Scheme == "unix" {
		c.base = "http://localhost"
		t.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", u.Path)
		}
	}
	c.client = &http.Client{Transport: t, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return c, nil
}
func (c *providerClient) Close() { c.client.CloseIdleConnections() }
func (c *providerClient) Get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.authorization != "" {
		req.Header.Set("Authorization", c.authorization)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s connection failed: %w", c.name, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s API returned HTTP %d (%s); check the API endpoint and authorization file", c.name, res.StatusCode, http.StatusText(res.StatusCode))
	}
	if next := res.Header.Get("X-Next-Page"); c.name == "Traefik" && next != "" && next != "0" && next != "1" {
		return fmt.Errorf("Traefik API returned a truncated/paginated snapshot; previous observations were retained")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("%s API response exceeds 8 MiB", c.name)
	}
	if err = json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("invalid %s API JSON response", c.name)
	}
	return nil
}
