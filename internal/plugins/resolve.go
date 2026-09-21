// Package plugins resolves a project's enabled plugin selections against a
// materialized registry snapshot into a deterministic, validated
// config.ResolvedPlan.
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shubzkothekar/devbox/internal/config"
	"github.com/shubzkothekar/devbox/internal/registry"
)

// catalogEntry is a single discovered plugin: its manifest plus the
// absolute directory it was loaded from.
type catalogEntry struct {
	dir      string
	manifest config.Manifest
}

// Resolve validates selections against the plugin manifests discovered in
// source and produces a deterministic, dependency-ordered
// config.ResolvedPlan.
//
// Discovery only considers plugins/*/plugin.yaml. Every discovered
// manifest's id must match its directory name. Every selection key must
// name a discovered plugin. Enabled plugins' requires must all be
// separately enabled, and no two enabled plugins may conflict. Enabled
// plugins are ordered so dependencies precede dependents, breaking ties
// lexicographically by id. Each enabled plugin's options are validated
// against its manifest's option schema (rejecting unknown options and type
// mismatches) and merged with schema defaults. Each enabled plugin's hook
// and command paths are validated to be relative, existing, regular files
// that do not escape the plugin's directory (directly or via a symlink).
func Resolve(source registry.Source, selections map[string]config.PluginSelection) (config.ResolvedPlan, error) {
	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return config.ResolvedPlan{}, err
	}

	for id := range selections {
		if _, ok := catalog[id]; !ok {
			return config.ResolvedPlan{}, fmt.Errorf("unknown plugin %q", id)
		}
	}

	enabledSet := make(map[string]bool)
	var enabledIDs []string
	for id, selection := range selections {
		if selection.Enabled {
			enabledSet[id] = true
			enabledIDs = append(enabledIDs, id)
		}
	}
	sort.Strings(enabledIDs)

	for _, id := range enabledIDs {
		entry := catalog[id]
		for _, req := range entry.manifest.Requires {
			if _, ok := catalog[req]; !ok {
				return config.ResolvedPlan{}, fmt.Errorf("plugin %q requires unknown plugin %q", id, req)
			}
			if !enabledSet[req] {
				return config.ResolvedPlan{}, fmt.Errorf("plugin %q requires %q, but %q is not enabled", id, req, req)
			}
		}
		for _, conflict := range entry.manifest.Conflicts {
			if enabledSet[conflict] {
				return config.ResolvedPlan{}, fmt.Errorf("plugin %q conflicts with enabled plugin %q", id, conflict)
			}
		}
	}

	order, err := topologicalOrder(catalog, enabledIDs)
	if err != nil {
		return config.ResolvedPlan{}, err
	}

	plan := config.ResolvedPlan{
		Version: 1,
		Source:  config.ResolvedSource{Root: source.Root, Mode: source.Mode},
	}
	for _, id := range order {
		entry := catalog[id]
		resolved, err := resolvePlugin(id, entry, selections[id])
		if err != nil {
			return config.ResolvedPlan{}, err
		}
		plan.Plugins = append(plan.Plugins, resolved)
	}

	return plan, nil
}

// discoverCatalog reads every plugins/<dir>/plugin.yaml under root and
// strictly decodes it, requiring the manifest's id to match its directory
// name.
func discoverCatalog(root string) (map[string]catalogEntry, error) {
	pluginsDir := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]catalogEntry{}, nil
		}
		return nil, fmt.Errorf("read registry plugins directory %s: %w", pluginsDir, err)
	}

	catalog := make(map[string]catalogEntry, len(entries))
	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		dirName := dirEntry.Name()
		pluginDir := filepath.Join(pluginsDir, dirName)
		manifestPath := filepath.Join(pluginDir, "plugin.yaml")

		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", manifestPath, err)
		}

		manifest, err := decodeManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", manifestPath, err)
		}

		if manifest.ID != dirName {
			return nil, fmt.Errorf("plugin directory %q: manifest id %q does not match directory name", dirName, manifest.ID)
		}

		catalog[manifest.ID] = catalogEntry{dir: pluginDir, manifest: manifest}
	}

	return catalog, nil
}

func decodeManifest(path string) (config.Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return config.Manifest{}, err
	}
	defer f.Close()

	var manifest config.Manifest
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return config.Manifest{}, err
	}
	return manifest, nil
}

// topologicalOrder orders ids so that every plugin's requires precede it,
// breaking ties among simultaneously ready plugins lexicographically.
func topologicalOrder(catalog map[string]catalogEntry, ids []string) ([]string, error) {
	remaining := make(map[string]bool, len(ids))
	for _, id := range ids {
		remaining[id] = true
	}

	resolved := make(map[string]bool, len(ids))
	order := make([]string, 0, len(ids))

	for len(order) < len(ids) {
		var candidates []string
		for id := range remaining {
			ready := true
			for _, req := range catalog[id].manifest.Requires {
				if remaining[req] {
					ready = false
					break
				}
			}
			if ready {
				candidates = append(candidates, id)
			}
		}
		if len(candidates) == 0 {
			var stuck []string
			for id := range remaining {
				stuck = append(stuck, id)
			}
			sort.Strings(stuck)
			return nil, fmt.Errorf("dependency cycle detected among plugins: %s", strings.Join(stuck, ", "))
		}
		sort.Strings(candidates)
		next := candidates[0]
		order = append(order, next)
		resolved[next] = true
		delete(remaining, next)
	}

	return order, nil
}

func resolvePlugin(id string, entry catalogEntry, selection config.PluginSelection) (config.ResolvedPlugin, error) {
	options, err := resolveOptions(id, entry.manifest, selection)
	if err != nil {
		return config.ResolvedPlugin{}, err
	}

	resolved := config.ResolvedPlugin{
		ID:      id,
		Root:    entry.dir,
		Options: options,
	}

	if entry.manifest.Hooks.Build != "" {
		relPath, err := validateRelPath(entry.dir, entry.manifest.Hooks.Build)
		if err != nil {
			return config.ResolvedPlugin{}, fmt.Errorf("plugin %q: hooks.build: %w", id, err)
		}
		resolved.Build = relPath
	}
	if entry.manifest.Hooks.Start != "" {
		relPath, err := validateRelPath(entry.dir, entry.manifest.Hooks.Start)
		if err != nil {
			return config.ResolvedPlugin{}, fmt.Errorf("plugin %q: hooks.start: %w", id, err)
		}
		resolved.Start = relPath
	}

	if len(entry.manifest.Commands) > 0 {
		resolved.Commands = make(map[string]config.ResolvedCommand, len(entry.manifest.Commands))
		for name, cmd := range entry.manifest.Commands {
			relPath, err := validateRelPath(entry.dir, cmd.Path)
			if err != nil {
				return config.ResolvedPlugin{}, fmt.Errorf("plugin %q: commands.%s: %w", id, name, err)
			}
			resolved.Commands[name] = config.ResolvedCommand{Path: relPath, User: cmd.User}
		}
	}

	return resolved, nil
}

func resolveOptions(id string, manifest config.Manifest, selection config.PluginSelection) (map[string]any, error) {
	for name := range selection.Options {
		if _, ok := manifest.Options[name]; !ok {
			return nil, fmt.Errorf("plugin %q: unknown option %q", id, name)
		}
	}

	if len(manifest.Options) == 0 {
		return map[string]any{}, nil
	}

	options := make(map[string]any, len(manifest.Options))
	for name, schema := range manifest.Options {
		value, overridden := selection.Options[name]
		if !overridden {
			value = schema.Default
		}
		if err := checkOptionType(schema.Type, value); err != nil {
			return nil, fmt.Errorf("plugin %q: option %q must be of type %s", id, name, schema.Type)
		}
		options[name] = value
	}

	return options, nil
}

func checkOptionType(schemaType string, value any) error {
	if value == nil {
		return nil
	}
	switch schemaType {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "integer":
		switch value.(type) {
		case int, int64, uint, uint64:
		default:
			return fmt.Errorf("expected integer, got %T", value)
		}
	case "number":
		switch value.(type) {
		case int, int64, uint, uint64, float32, float64:
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	default:
		return fmt.Errorf("unknown option type %q", schemaType)
	}
	return nil
}

// validateRelPath validates that rel names a relative, existing, regular
// file located within pluginRoot, and that it does not escape pluginRoot
// directly (via ".." segments) or indirectly (via a symlink pointing
// outside pluginRoot). It returns rel's cleaned form on success.
func validateRelPath(pluginRoot, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative, not absolute", rel)
	}

	cleanRel := filepath.Clean(rel)
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes plugin directory", rel)
	}

	candidate := filepath.Join(pluginRoot, cleanRel)
	if escapes(pluginRoot, candidate) {
		return "", fmt.Errorf("path %q escapes plugin directory", rel)
	}

	// EvalSymlinks resolves every path component, not just the leaf, so a
	// symlink at any intermediate directory (not only the final file) is
	// caught here. Resolving pluginRoot too keeps the comparison anchored
	// to the same canonical (fully symlink-resolved) coordinate space.
	canonicalRoot, err := filepath.EvalSymlinks(pluginRoot)
	if err != nil {
		return "", fmt.Errorf("resolve plugin root %q: %w", pluginRoot, err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", rel, err)
	}
	if escapes(canonicalRoot, resolvedTarget) {
		return "", fmt.Errorf("path %q: symlink target escapes plugin directory", rel)
	}

	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", rel)
	}

	return cleanRel, nil
}

// escapes reports whether candidate resolves outside root once both are
// expressed relative to each other.
func escapes(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
