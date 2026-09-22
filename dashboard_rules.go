package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
)

func compileDashboardRule(rule string) (cel.Program, error) {
	if len(rule) > 4096 {
		return nil, fmt.Errorf("CEL rule must be at most 4096 bytes")
	}
	options := []cel.EnvOption{cel.ParserRecursionLimit(32), cel.ParserExpressionSizeLimit(4096)}
	for _, name := range []string{"id", "name", "description", "url", "host", "path", "category", "health", "lifecycle"} {
		options = append(options, cel.Variable(name, cel.StringType))
	}
	for _, name := range []string{"urls", "provider_types", "provider_ids"} {
		options = append(options, cel.Variable(name, cel.ListType(cel.StringType)))
	}
	options = append(options, cel.Variable("favorite", cel.BoolType), cel.Variable("hidden", cel.BoolType), cel.Variable("facts", cel.MapType(cel.StringType, cel.StringType)))
	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(rule)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("CEL rule must return a boolean")
	}
	return env.Program(ast, cel.CostLimit(10000), cel.InterruptCheckFrequency(100))
}
func ruleValues(a application, kinds map[string]string) map[string]any {
	host, path := "", ""
	if u, err := url.Parse(a.URL); err == nil {
		host = u.Hostname()
		path = u.Path
	}
	facts := map[string]string{}
	for key, f := range a.Fields {
		facts[key] = f.Value
	}
	ids, providerTypes := []string{}, []string{}
	for _, id := range a.ActiveSources {
		kind := kinds[id]
		if strings.HasPrefix(id, "manual:") {
			kind = "manual"
		}
		if kind == "" {
			continue
		}
		ids = append(ids, id)
		if !contains(providerTypes, kind) {
			providerTypes = append(providerTypes, kind)
		}
	}
	return map[string]any{"id": a.ID, "name": a.Name, "description": a.Description, "url": a.URL, "urls": a.URLs, "host": host, "path": path, "category": a.Category, "health": a.Health, "lifecycle": a.Lifecycle, "favorite": a.Favorite, "hidden": a.Hidden, "facts": facts, "provider_ids": ids, "provider_types": providerTypes}
}
func evaluateDashboardRule(ctx context.Context, rule string, apps []application, providers []provider) ([]application, error) {
	program, err := compileDashboardRule(rule)
	if err != nil {
		return nil, err
	}
	kinds := map[string]string{}
	for _, p := range providers {
		kinds[p.ID] = p.Type
	}
	matches := []application{}
	for _, a := range apps {
		if a.Hidden || a.Lifecycle != "present" {
			continue
		}
		if err = ctx.Err(); err != nil {
			return nil, fmt.Errorf("rule evaluation deadline exceeded")
		}
		value, _, err := program.ContextEval(ctx, ruleValues(a, kinds))
		if err != nil {
			return nil, fmt.Errorf("rule failed for %s: %w", a.Name, err)
		}
		if value == types.True {
			matches = append(matches, a)
		}
	}
	return matches, nil
}
func (s *server) previewDashboardRule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Rule string `json:"rule"`
	}
	if !body(w, r, &input) {
		return
	}
	apps, err := s.apps()
	if err != nil {
		dbError(w, err)
		return
	}
	ps, err := s.providers()
	if err != nil {
		dbError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	matches, err := evaluateDashboardRule(ctx, input.Rule, apps, ps)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	result := []map[string]string{}
	for _, a := range matches {
		result = append(result, map[string]string{"id": a.ID, "name": a.Name, "url": a.URL, "category": a.Category})
	}
	reply(w, map[string]any{"matches": result, "count": len(result)})
}
func (s *server) applyDashboardRules(ctx context.Context) error {
	dashboards, err := s.dashboards()
	if err != nil {
		return err
	}
	active := false
	for _, d := range dashboards {
		if d.AutoRule != nil && *d.AutoRule != "" {
			active = true
			break
		}
	}
	if !active {
		return nil
	}
	apps, err := s.apps()
	if err != nil {
		return err
	}
	ps, err := s.providers()
	if err != nil {
		return err
	}
	for _, d := range dashboards {
		if d.AutoRule == nil || *d.AutoRule == "" {
			continue
		}
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		matches, ruleErr := evaluateDashboardRule(bounded, *d.AutoRule, apps, ps)
		cancel()
		if ruleErr != nil {
			if _, err = s.db.Exec("UPDATE dashboards SET rule_error=? WHERE id=? AND revision=?", ruleErr.Error(), d.ID, *d.Revision); err != nil {
				return err
			}
			continue
		}
		if err = s.appendRuleMatches(d, matches); err != nil {
			return err
		}
	}
	return nil
}
func (s *server) appendRuleMatches(d dashboard, matches []application) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	added := false
	for _, a := range matches {
		if contains(d.Items, a.ID) || contains(d.Excluded, a.ID) {
			continue
		}
		if len(d.Items) >= 1000 {
			_, err = tx.Exec("UPDATE dashboards SET rule_error=? WHERE id=? AND revision=?", "Dashboard limit of 1000 apps reached", d.ID, *d.Revision)
			if err != nil {
				return err
			}
			return tx.Commit()
		}
		var exists int
		if err = tx.QueryRow("SELECT 1 FROM applications WHERE id=?", a.ID).Scan(&exists); err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		d.Items = append(d.Items, a.ID)
		d.Layout.Tiles[a.ID] = appendTile(d.Layout, d.AutoSection)
		added = true
	}
	if !added {
		_, err = tx.Exec("UPDATE dashboards SET rule_error='' WHERE id=? AND revision=?", d.ID, *d.Revision)
	} else {
		if err = normalizeLayout(&d); err != nil {
			_, e := tx.Exec("UPDATE dashboards SET rule_error=? WHERE id=? AND revision=?", err.Error(), d.ID, *d.Revision)
			if e != nil {
				return e
			}
			return tx.Commit()
		}
		d.Items = layoutOrder(d)
		_, err = tx.Exec("UPDATE dashboards SET items=?,layout=?,rule_error='',revision=revision+1 WHERE id=? AND revision=?", encode(d.Items), encode(d.Layout), d.ID, *d.Revision)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *server) refreshDashboardRules(ctx context.Context) {
	if err := s.applyDashboardRules(ctx); err != nil {
		log.Printf("Dashboard auto-add: %v", err)
	}
}

func decodeDashboard(d *dashboard, items, layout, excluded string) error {
	if err := json.Unmarshal([]byte(items), &d.Items); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(layout), &d.Layout); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(excluded), &d.Excluded); err != nil {
		return err
	}
	return normalizeLayout(d)
}
