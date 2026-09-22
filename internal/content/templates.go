package content

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

func readDocuments(root string) (map[string]Spec, map[string][]map[string]any, error) {
	docs := map[string]Spec{}
	lists := map[string][]map[string]any{}
	for _, rel := range RequiredModels {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, fmt.Errorf("content %s: %w", rel, err)
		}
		var value any
		if err = yaml.Unmarshal(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("content %s: %w", rel, err)
		}
		table := strings.TrimSuffix(filepath.Base(rel), ".yaml")
		docs[table] = Spec{Value: value}
		seq, ok := value.([]any)
		if !ok {
			continue
		}
		for _, item := range seq {
			v, ok := item.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("content %s contains a non-map entry", rel)
			}
			lists[rel] = append(lists[rel], v)
		}
		if table == "skills" {
			resolved, err := resolveTemplates(lists[rel])
			if err != nil {
				return nil, nil, err
			}
			lists[rel] = resolved
		}
	}
	return docs, lists, nil
}
func resolveTemplates(entries []map[string]any) ([]map[string]any, error) {
	byName := map[string]map[string]any{}
	for _, e := range entries {
		name := asStr(e["name"], "")
		if name == "" || byName[name] != nil {
			return nil, fmt.Errorf("skill name missing or duplicated: %q", name)
		}
		byName[name] = e
	}
	resolved := map[string]map[string]any{}
	active := map[string]bool{}
	var resolve func(string) (map[string]any, error)
	resolve = func(name string) (map[string]any, error) {
		if active[name] {
			return nil, fmt.Errorf("skill template cycle at %s", name)
		}
		if r := resolved[name]; r != nil {
			return r, nil
		}
		e := byName[name]
		if e == nil {
			return nil, fmt.Errorf("unknown skill template %s", name)
		}
		active[name] = true
		result := map[string]any{}
		if base := asStr(e["base"], ""); base != "" {
			r, err := resolve(base)
			if err != nil {
				return nil, err
			}
			result = mergeMaps(result, r)
		}
		result = mergeMaps(result, e)
		delete(result, "base")
		delete(result, "template")
		delete(active, name)
		resolved[name] = result
		return result, nil
	}
	out := []map[string]any{}
	for _, e := range entries {
		r, err := resolve(asStr(e["name"], ""))
		if err != nil {
			return nil, err
		}
		if !asBool(e["template"], false) {
			out = append(out, r)
		}
	}
	return out, nil
}
func mergeMaps(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		if m, ok := v.(map[string]any); ok {
			out[k] = mergeMaps(nil, m)
		} else {
			out[k] = v
		}
	}
	for k, v := range over {
		if m, ok := v.(map[string]any); ok {
			out[k] = mergeMaps(asMap(out[k]), m)
		} else {
			out[k] = v
		}
	}
	return out
}
