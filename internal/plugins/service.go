package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shubzkothekar/devbox/internal/config"
	"github.com/shubzkothekar/devbox/internal/registry"
)

// CatalogPlugin represents a plugin discovered in the registry catalog.
type CatalogPlugin struct {
	ID          string
	Description string
	Version     string
}

// InstalledPlugin represents an enabled plugin in the project.
type InstalledPlugin struct {
	ID      string
	Version string
	Enabled bool
}

// Service provides lifecycle operations for DevBox project plugins.
type Service struct {
	Root string
}

// NewService constructs a Service for the DevBox project rooted at root.
func NewService(root string) Service {
	return Service{Root: root}
}

// SourceMode returns the active registry mode ("git" or "path").
func (s Service) SourceMode(ctx context.Context) (string, error) {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return "", err
	}
	if state.Override != nil {
		return "path", nil
	}
	return "git", nil
}

// List discovers and returns all plugins in the materialized registry catalog.
// It uses read-only materialization without network access.
func (s Service) List(ctx context.Context) ([]CatalogPlugin, error) {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return nil, err
	}

	source, err := registry.Materialize(ctx, state, registry.ReadOnly)
	if err != nil {
		return nil, err
	}

	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return nil, err
	}

	var list []CatalogPlugin
	for id, entry := range catalog {
		list = append(list, CatalogPlugin{
			ID:          id,
			Description: entry.manifest.Description,
			Version:     entry.manifest.Version,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// Installed returns all enabled plugins in the project.
// It uses read-only materialization to look up versions.
func (s Service) Installed(ctx context.Context) ([]InstalledPlugin, error) {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return nil, err
	}

	source, err := registry.Materialize(ctx, state, registry.ReadOnly)
	if err != nil {
		return nil, err
	}

	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return nil, err
	}

	var list []InstalledPlugin
	for id, sel := range state.Config.Plugins {
		if !sel.Enabled {
			continue
		}
		var version string
		if entry, ok := catalog[id]; ok {
			version = entry.manifest.Version
		}
		list = append(list, InstalledPlugin{
			ID:      id,
			Version: version,
			Enabled: true,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// Install materializes the registry, validates that id exists and its dependency closure
// is satisfied, enables the plugin in devbox.plugins.yml, updates the lock file (in Git mode),
// and writes .generated/plugins/plan.json atomically.
func (s Service) Install(ctx context.Context, id string) error {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return err
	}

	source, err := registry.Materialize(ctx, state, registry.Install)
	if err != nil {
		return err
	}

	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return err
	}

	if _, ok := catalog[id]; !ok {
		return fmt.Errorf("unknown plugin %q", id)
	}

	updatedPlugins := make(map[string]config.PluginSelection, len(state.Config.Plugins)+1)
	for k, v := range state.Config.Plugins {
		updatedPlugins[k] = v
	}
	sel := updatedPlugins[id]
	sel.Enabled = true
	updatedPlugins[id] = sel

	plan, err := Resolve(source, updatedPlugins)
	if err != nil {
		return err
	}

	state.Config.Plugins = updatedPlugins
	configPath := filepath.Join(s.Root, "devbox.plugins.yml")
	if err := config.WriteYAMLAtomic(configPath, state.Config); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if source.Mode == "git" && state.Override == nil {
		if err := s.writeLock(ctx, state, source.Root); err != nil {
			return err
		}
	}

	return s.writePlan(plan)
}

// Uninstall rejects uninstalling a plugin if any enabled plugin requires it.
// Otherwise, it disables the plugin, retains the existing lock file, and rewrites plan.json.
func (s Service) Uninstall(ctx context.Context, id string) error {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return err
	}

	source, err := registry.Materialize(ctx, state, registry.ReadOnly)
	if err != nil {
		return err
	}

	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return err
	}

	// Reject if any remaining enabled plugin requires this plugin.
	for otherID, otherSel := range state.Config.Plugins {
		if otherID == id || !otherSel.Enabled {
			continue
		}
		if entry, ok := catalog[otherID]; ok {
			for _, req := range entry.manifest.Requires {
				if req == id {
					return fmt.Errorf("cannot uninstall %q: enabled plugin %q requires it", id, otherID)
				}
			}
		}
	}

	updatedPlugins := make(map[string]config.PluginSelection, len(state.Config.Plugins))
	for k, v := range state.Config.Plugins {
		updatedPlugins[k] = v
	}
	if sel, exists := updatedPlugins[id]; exists {
		sel.Enabled = false
		updatedPlugins[id] = sel
	}

	plan, err := Resolve(source, updatedPlugins)
	if err != nil {
		return err
	}

	state.Config.Plugins = updatedPlugins
	configPath := filepath.Join(s.Root, "devbox.plugins.yml")
	if err := config.WriteYAMLAtomic(configPath, state.Config); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	return s.writePlan(plan)
}

// Update re-materializes the latest commit for the configured Git ref, validates all
// enabled plugins, rewrites the lock file, and rewrites plan.json. If id is non-empty,
// it validates that id is enabled and remains present in the catalog.
func (s Service) Update(ctx context.Context, id string) error {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return err
	}

	source, err := registry.Materialize(ctx, state, registry.Update)
	if err != nil {
		return err
	}

	catalog, err := discoverCatalog(source.Root)
	if err != nil {
		return err
	}

	if id != "" {
		if _, ok := catalog[id]; !ok {
			return fmt.Errorf("plugin %q not found in registry", id)
		}
		if sel, ok := state.Config.Plugins[id]; !ok || !sel.Enabled {
			return fmt.Errorf("plugin %q is not enabled", id)
		}
	}

	plan, err := Resolve(source, state.Config.Plugins)
	if err != nil {
		return err
	}

	if source.Mode == "git" && state.Override == nil {
		if err := s.writeLock(ctx, state, source.Root); err != nil {
			return err
		}
	}

	return s.writePlan(plan)
}

// ResolveGenerated resolves the current plugin configuration using the locked snapshot
// without network access and atomically writes .generated/plugins/plan.json.
func (s Service) ResolveGenerated(ctx context.Context) (config.ResolvedPlan, error) {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return config.ResolvedPlan{}, err
	}

	source, err := registry.Materialize(ctx, state, registry.ReadOnly)
	if err != nil {
		return config.ResolvedPlan{}, err
	}

	plan, err := Resolve(source, state.Config.Plugins)
	if err != nil {
		return config.ResolvedPlan{}, err
	}

	if err := s.writePlan(plan); err != nil {
		return config.ResolvedPlan{}, err
	}

	return plan, nil
}

// FindCommand looks up a command declared by an enabled plugin, defaulting the executing user to "devbox".
func (s Service) FindCommand(ctx context.Context, pluginID, command string) (config.ResolvedPlugin, config.ResolvedCommand, error) {
	state, err := config.LoadProject(s.Root)
	if err != nil {
		return config.ResolvedPlugin{}, config.ResolvedCommand{}, err
	}

	source, err := registry.Materialize(ctx, state, registry.ReadOnly)
	if err != nil {
		return config.ResolvedPlugin{}, config.ResolvedCommand{}, err
	}

	plan, err := Resolve(source, state.Config.Plugins)
	if err != nil {
		return config.ResolvedPlugin{}, config.ResolvedCommand{}, err
	}

	for _, p := range plan.Plugins {
		if p.ID == pluginID {
			cmd, ok := p.Commands[command]
			if !ok {
				return config.ResolvedPlugin{}, config.ResolvedCommand{}, fmt.Errorf("plugin %q has no command %q", pluginID, command)
			}
			if cmd.User == "" {
				cmd.User = "devbox"
			}
			return p, cmd, nil
		}
	}
	return config.ResolvedPlugin{}, config.ResolvedCommand{}, fmt.Errorf("plugin %q is not enabled or not found", pluginID)
}

func (s Service) writePlan(plan config.ResolvedPlan) error {
	planDir := filepath.Join(s.Root, ".generated", "plugins")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		return fmt.Errorf("create plan directory %s: %w", planDir, err)
	}
	planPath := filepath.Join(planDir, "plan.json")
	if err := config.WriteJSONAtomic(planPath, plan); err != nil {
		return fmt.Errorf("write %s: %w", planPath, err)
	}
	return nil
}

func (s Service) writeLock(ctx context.Context, state config.ProjectState, sourceRoot string) error {
	commit, err := registry.SnapshotCommit(ctx, sourceRoot)
	if err != nil {
		return fmt.Errorf("resolve snapshot commit: %w", err)
	}
	lock := config.RegistryLock{
		Registry: config.LockedRegistry{
			URL:    state.Config.Registry.URL,
			Ref:    state.Config.Registry.Ref,
			Commit: commit,
		},
	}
	lockPath := filepath.Join(s.Root, "devbox.plugins.lock.yml")
	if err := config.WriteYAMLAtomic(lockPath, lock); err != nil {
		return fmt.Errorf("write %s: %w", lockPath, err)
	}
	return nil
}
