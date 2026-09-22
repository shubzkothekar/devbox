package config

// ProjectConfig is the strict, typed representation of a project's
// devbox.plugins.yml file: the configured registry source and the set of
// enabled/disabled plugin selections.
type ProjectConfig struct {
	Registry RegistryConfig             `yaml:"registry"`
	Plugins  map[string]PluginSelection `yaml:"plugins"`
}

// RegistryConfig declares the canonical Git registry source for plugin
// resolution.
type RegistryConfig struct {
	Source string `yaml:"source"`
	URL    string `yaml:"url"`
	Ref    string `yaml:"ref"`
}

// PluginSelection is a single plugin's enabled state and user-supplied
// options as declared in devbox.plugins.yml.
type PluginSelection struct {
	Enabled bool           `yaml:"enabled"`
	Options map[string]any `yaml:"options,omitempty"`
}

// RegistryLock is the strict, typed representation of a project's
// devbox.plugins.lock.yml file: the exact resolved registry commit.
type RegistryLock struct {
	Registry LockedRegistry `yaml:"registry"`
}

// LockedRegistry pins the registry URL, ref, and resolved commit used for
// reproducible builds.
type LockedRegistry struct {
	URL    string `yaml:"url"`
	Ref    string `yaml:"ref"`
	Commit string `yaml:"commit"`
}

// LocalOverride is the strict, typed representation of a project's
// (gitignored) devbox.plugins.local.yml file, which replaces the configured
// Git registry source with a local path for plugin development.
type LocalOverride struct {
	Registry LocalRegistry `yaml:"registry"`
}

// LocalRegistry declares a non-reproducible, local-path registry source.
type LocalRegistry struct {
	Source string `yaml:"source"`
	Path   string `yaml:"path"`
}

// Manifest is the strict, typed representation of a plugin's plugin.yaml
// contract as declared in the devbox-registry repository.
type Manifest struct {
	ID           string                   `yaml:"id"`
	Name         string                   `yaml:"name,omitempty"`
	Version      string                   `yaml:"version"`
	Description  string                   `yaml:"description,omitempty"`
	Requires     []string                 `yaml:"requires,omitempty"`
	Conflicts    []string                 `yaml:"conflicts,omitempty"`
	Options      map[string]OptionSchema  `yaml:"options,omitempty"`
	Hooks        HookPaths                `yaml:"hooks,omitempty"`
	Commands     map[string]CommandSchema `yaml:"commands,omitempty"`
	DevContainer DevContainerContribution `yaml:"devcontainer,omitempty"`
}

// OptionSchema declares a single plugin option's type and default value.
type OptionSchema struct {
	Type    string `yaml:"type"`
	Default any    `yaml:"default,omitempty"`
}

// HookPaths declares the relative script paths invoked during image build
// and container startup, relative to the plugin directory.
type HookPaths struct {
	Build string `yaml:"build,omitempty"`
	Start string `yaml:"start,omitempty"`
}

// CommandSchema declares a developer-invoked plugin command's relative
// script path and the user it runs as.
type CommandSchema struct {
	Path string `yaml:"path"`
	User string `yaml:"user,omitempty"`
}

// DevContainerContribution declares additive Dev Container configuration
// contributed by a plugin.
type DevContainerContribution struct {
	Extensions         []string          `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Mounts             []string          `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	ContainerEnv       map[string]string `yaml:"containerEnv,omitempty" json:"containerEnv,omitempty"`
	PostCreateCommands []string          `yaml:"postCreateCommands,omitempty" json:"postCreateCommands,omitempty"`
	ForwardPorts       []int             `yaml:"forwardPorts,omitempty" json:"forwardPorts,omitempty"`
}

// ResolvedPlan is the normalized, deterministic, JSON-safe output of plugin
// resolution: the materialized registry source plus the ordered set of
// enabled plugins with validated relative hook/command references. Later
// build, startup, and Dev Container generation tasks consume this shape.
type ResolvedPlan struct {
	Version int              `json:"version"`
	Source  ResolvedSource   `json:"source"`
	Plugins []ResolvedPlugin `json:"plugins"`
}

// PluginIDs returns the resolved plugins' IDs in plan (dependency-sorted)
// order.
func (p ResolvedPlan) PluginIDs() []string {
	ids := make([]string, len(p.Plugins))
	for i, plugin := range p.Plugins {
		ids[i] = plugin.ID
	}
	return ids
}

// ResolvedSource records the materialized registry root and source mode
// ("git" or "path") that produced a ResolvedPlan.
type ResolvedSource struct {
	Root string `json:"root"`
	Mode string `json:"mode"`
}

// ResolvedPlugin is a single enabled plugin's identity, resolved option
// values, and validated relative hook/command references, in the order it
// should be applied.
type ResolvedPlugin struct {
	ID           string                     `json:"id"`
	Root         string                     `json:"root"`
	Options      map[string]any             `json:"options"`
	Build        string                     `json:"build,omitempty"`
	Start        string                     `json:"start,omitempty"`
	Commands     map[string]ResolvedCommand `json:"commands,omitempty"`
	DevContainer DevContainerContribution   `json:"devcontainer,omitempty"`
}

// ResolvedCommand is a single developer-invoked plugin command's validated
// relative script path and the user it runs as.
type ResolvedCommand struct {
	Path string `json:"path"`
	User string `json:"user,omitempty"`
}

// ProjectState is the fully loaded configuration for a DevBox project
// rooted at Root: the required project configuration plus the optional
// lock file and local registry override, when present.
type ProjectState struct {
	Root     string
	Config   ProjectConfig
	Lock     *RegistryLock
	Override *LocalOverride
}
