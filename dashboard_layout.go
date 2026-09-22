package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

type dashboardSection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type dashboardTile struct {
	Section string `json:"section"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
}
type dashboardLayout struct {
	Sections []dashboardSection       `json:"sections"`
	Tiles    map[string]dashboardTile `json:"tiles"`
}

func tilesOverlap(a, b dashboardTile) bool {
	return a.Section == b.Section && a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
func appendTile(layout *dashboardLayout, section string) dashboardTile {
	bottom := 0
	for _, t := range layout.Tiles {
		if t.Section == section && t.Y+t.H > bottom {
			bottom = t.Y + t.H
		}
	}
	for _, y := range []int{max(0, bottom-6), bottom} {
		for x := 0; x <= 8; x += 4 {
			candidate := dashboardTile{Section: section, X: x, Y: y, W: 4, H: 6}
			free := true
			for _, other := range layout.Tiles {
				if tilesOverlap(candidate, other) {
					free = false
					break
				}
			}
			if free {
				return candidate
			}
		}
	}
	return dashboardTile{Section: section, Y: bottom, W: 4, H: 6}
}
func normalizeLayout(d *dashboard) error {
	if d.Layout == nil {
		d.Layout = &dashboardLayout{}
	}
	l := d.Layout
	if len(l.Sections) == 0 {
		l.Sections = []dashboardSection{{ID: "main", Name: "General"}}
	}
	if len(l.Sections) > 32 {
		return fmt.Errorf("a dashboard can have at most 32 sections")
	}
	sections := map[string]bool{}
	for _, s := range l.Sections {
		if !validSlug(s.ID) || !required(s.Name, 80) || sections[s.ID] {
			return fmt.Errorf("sections need unique IDs and names of up to 80 characters")
		}
		sections[s.ID] = true
	}
	if l.Tiles == nil {
		l.Tiles = map[string]dashboardTile{}
	}
	for id, t := range l.Tiles {
		if !contains(d.Items, id) {
			delete(l.Tiles, id)
			continue
		}
		if !sections[t.Section] || t.X < 0 || t.Y < 0 || t.Y > 10000 || t.W < 3 || t.W > 12 || t.H < 4 || t.H > 20 || t.X+t.W > 12 {
			return fmt.Errorf("tiles need a valid section, 12-column bounds, width 3–12, height 4–20, and nonnegative positions")
		}
	}
	for _, id := range d.Items {
		if _, ok := l.Tiles[id]; !ok {
			l.Tiles[id] = appendTile(l, l.Sections[0].ID)
		}
	}
	for _, t := range l.Tiles {
		if t.Y > 10000 {
			return fmt.Errorf("dashboard layout exceeds the row limit")
		}
	}
	for i, id := range d.Items {
		for _, other := range d.Items[i+1:] {
			if tilesOverlap(l.Tiles[id], l.Tiles[other]) {
				return fmt.Errorf("dashboard tiles overlap; move or resize the overlapping cards")
			}
		}
	}
	if d.AutoSection == "" {
		d.AutoSection = l.Sections[0].ID
	}
	if !sections[d.AutoSection] {
		return fmt.Errorf("auto-add destination must be an existing section")
	}
	return nil
}
func layoutOrder(d dashboard) []string {
	out := append([]string{}, d.Items...)
	sectionIndex := map[string]int{}
	for i, s := range d.Layout.Sections {
		sectionIndex[s.ID] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := d.Layout.Tiles[out[i]], d.Layout.Tiles[out[j]]
		if a.Section != b.Section {
			return sectionIndex[a.Section] < sectionIndex[b.Section]
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	return out
}

// Layout/exclusion references move with alias consolidation and app deletion.
func rewriteDashboardReferences(tx *sql.Tx, ids []string, canonical string) error {
	rows, err := tx.Query("SELECT id,items,layout,excluded FROM dashboards")
	if err != nil {
		return err
	}
	type update struct{ id, items, layout, excluded string }
	updates := []update{}
	for rows.Next() {
		var id, itemsRaw, layoutRaw, excludedRaw string
		if err = rows.Scan(&id, &itemsRaw, &layoutRaw, &excludedRaw); err != nil {
			rows.Close()
			return err
		}
		var items, excluded []string
		var layout dashboardLayout
		if err = json.Unmarshal([]byte(itemsRaw), &items); err == nil {
			err = json.Unmarshal([]byte(layoutRaw), &layout)
		}
		if err == nil {
			err = json.Unmarshal([]byte(excludedRaw), &excluded)
		}
		if err != nil {
			rows.Close()
			return err
		}
		changed := false
		rewrite := func(old []string) []string {
			result := []string{}
			for _, item := range old {
				if contains(ids, item) {
					changed = true
					if canonical == "" {
						continue
					}
					item = canonical
					if contains(result, item) {
						continue
					}
				}
				result = append(result, item)
			}
			return result
		}
		items = rewrite(items)
		excluded = rewrite(excluded)
		var kept *dashboardTile
		if t, ok := layout.Tiles[canonical]; canonical != "" && ok {
			kept = &t
		}
		for _, old := range ids {
			if t, ok := layout.Tiles[old]; ok {
				changed = true
				if kept == nil {
					copy := t
					kept = &copy
				}
				delete(layout.Tiles, old)
			}
		}
		if canonical != "" && kept != nil {
			layout.Tiles[canonical] = *kept
		}
		if changed {
			updates = append(updates, update{id, encode(items), encode(layout), encode(excluded)})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, u := range updates {
		if _, err = tx.Exec("UPDATE dashboards SET items=?,layout=?,excluded=?,revision=revision+1 WHERE id=?", u.items, u.layout, u.excluded, u.id); err != nil {
			return err
		}
	}
	return nil
}
