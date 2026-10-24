package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type stewardExport struct {
	Schema   string `json:"schema_version"`
	Kind     string `json:"kind"`
	Host     string `json:"host_id"`
	Revision string `json:"revision"`
	Observed int64  `json:"observed_at"`
	Complete bool   `json:"complete"`
	Sources  []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Complete bool   `json:"complete"`
	} `json:"sources"`
	Resources []struct {
		Kind      string  `json:"kind"`
		ID        string  `json:"id"`
		Binding   string  `json:"binding"`
		Scope     string  `json:"scope"`
		Name      string  `json:"name"`
		Enabled   bool    `json:"enabled"`
		Ownership string  `json:"ownership"`
		Observed  string  `json:"observed"`
		URL       *string `json:"public_url"`
	} `json:"resources"`
}

func validStewardEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "file" || u.Host != "" || !filepath.IsAbs(stewardFilePath(u.Path)) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Path, "\x00\r\n") {
		return fmt.Errorf("use an absolute file:// export URL")
	}
	return nil
}

func stewardFilePath(path string) string {
	if strings.HasPrefix(path, "/") && filepath.VolumeName(path[1:]) != "" {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

func stewardSnapshot(ctx context.Context, p provider) ([]observation, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if err := validStewardEndpoint(p.Endpoint); err != nil {
		return nil, "", err
	}
	u, _ := url.Parse(p.Endpoint)
	path := stewardFilePath(u.Path)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, "", fmt.Errorf("Steward export must be a regular file of at most 8 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("Steward export is unreadable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return nil, "", fmt.Errorf("Steward export exceeds its read budget")
	}
	var snapshot stewardExport
	if err := validateStewardJSON(data); err != nil {
		return nil, "", fmt.Errorf("invalid or duplicate-key Steward JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&snapshot); err != nil {
		return nil, "", fmt.Errorf("invalid Steward export schema")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, "", fmt.Errorf("Steward export must contain one JSON document")
	}
	if snapshot.Schema != "1" || snapshot.Kind != "steward.inventory" || !snapshot.Complete || snapshot.Host == "" || len(snapshot.Host) > 128 || len(snapshot.Resources) > 10000 || len(snapshot.Sources) > 256 {
		return nil, "", fmt.Errorf("Steward export is partial or incompatible; previous observations retained")
	}
	now := time.Now().Unix()
	if snapshot.Observed < now-15*60 || snapshot.Observed > now+300 {
		return nil, "", fmt.Errorf("Steward export is stale or future-dated; refresh the export")
	}
	for _, source := range snapshot.Sources {
		if !source.Complete {
			return nil, "", fmt.Errorf("Steward source coverage is partial; previous observations retained")
		}
	}
	observations := []observation{}
	seen := map[string]bool{}
	for _, r := range snapshot.Resources {
		if (r.Kind != "route" && r.Kind != "service") || (r.Scope != "user" && r.Scope != "system") || !required(r.ID, 64) || !required(r.Name, 100) || r.Ownership != "owned_definition" {
			return nil, "", fmt.Errorf("invalid Steward resource identity")
		}
		source := "steward:" + url.PathEscape(snapshot.Host) + ":" + r.Scope + ":" + r.Kind + ":" + url.PathEscape(r.Binding) + ":" + url.PathEscape(r.ID)
		if seen[source] {
			return nil, "", fmt.Errorf("duplicate Steward resource")
		}
		seen[source] = true
		facts := map[string]fact{"name": {r.Name, 60, "Steward"}, "description": {fmt.Sprintf("Steward %s · %s · %s", r.Kind, r.Scope, r.Observed), 40, "Steward"}}
		keys := []string{source}
		urls := []string{}
		if r.URL != nil && r.Enabled {
			u, err := validURL(*r.URL)
			if err != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, "", fmt.Errorf("invalid Steward public URL")
			}
			canonical := normalizedURL(*r.URL)
			facts["url"] = fact{canonical, 60, "Steward"}
			urls = append(urls, canonical)
			keys = append(keys, "url:"+canonical)
		}
		observations = append(observations, observation{Source: source, Keys: keys, Facts: facts, URLs: urls})
	}
	return observations, "Steward export snapshot; no control or ownership is transferred", nil
}

func validateStewardJSON(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	nodes := 0
	var visit func(int) error
	visit = func(depth int) error {
		nodes++
		if depth > 64 || nodes > 100000 {
			return fmt.Errorf("JSON budget exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				seen := map[string]bool{}
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return fmt.Errorf("duplicate object key")
					}
					seen[name] = true
					if err = visit(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for decoder.More() {
					if err = visit(depth + 1); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("unexpected delimiter")
			}
			_, err = decoder.Token()
			return err
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
