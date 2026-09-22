package devcontainer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shubzkothekar/devbox/internal/config"
)

// Merge merges the additive Dev Container configuration contributed by enabled plugins
// into the base devcontainer.json configuration.
//
// Extensions under customizations.vscode are deduplicated while preserving order.
// ForwardPorts are validated (1..65535) and deduplicated while preserving order.
// ContainerEnv entries are merged; identical values are accepted, conflicting values fail.
// Mounts are merged; identical definitions are deduplicated; distinct definitions targeting
// the same destination path fail.
// PostCreateCommands are appended in resolved plugin order.
// Other base properties are preserved as-is.
func Merge(base []byte, plan config.ResolvedPlan) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(base, &root); err != nil {
		return nil, fmt.Errorf("parse base devcontainer.json: %w", err)
	}
	if root == nil {
		root = make(map[string]any)
	}

	// 1. Extensions: customizations.vscode.extensions
	if err := mergeExtensions(root, plan); err != nil {
		return nil, err
	}

	// 2. ForwardPorts
	if err := mergeForwardPorts(root, plan); err != nil {
		return nil, err
	}

	// 3. ContainerEnv
	if err := mergeContainerEnv(root, plan); err != nil {
		return nil, err
	}

	// 4. Mounts
	if err := mergeMounts(root, plan); err != nil {
		return nil, err
	}

	// 5. PostCreateCommands
	if err := mergePostCreateCommands(root, plan); err != nil {
		return nil, err
	}

	merged, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged devcontainer: %w", err)
	}
	return append(merged, '\n'), nil
}

func mergeExtensions(root map[string]any, plan config.ResolvedPlan) error {
	var customizations map[string]any
	if rawCust, ok := root["customizations"]; ok && rawCust != nil {
		if cMap, ok := rawCust.(map[string]any); ok {
			customizations = cMap
		}
	}
	var vscode map[string]any
	if customizations != nil {
		if rawVS, ok := customizations["vscode"]; ok && rawVS != nil {
			if vsMap, ok := rawVS.(map[string]any); ok {
				vscode = vsMap
			}
		}
	}

	var existingExts []string
	if vscode != nil {
		if rawExts, ok := vscode["extensions"]; ok && rawExts != nil {
			if list, ok := rawExts.([]any); ok {
				for _, item := range list {
					if s, ok := item.(string); ok {
						existingExts = append(existingExts, s)
					}
				}
			} else if list, ok := rawExts.([]string); ok {
				existingExts = append(existingExts, list...)
			}
		}
	}

	seen := make(map[string]bool)
	var finalExts []string
	for _, ext := range existingExts {
		if !seen[ext] {
			seen[ext] = true
			finalExts = append(finalExts, ext)
		}
	}

	for _, p := range plan.Plugins {
		for _, ext := range p.DevContainer.Extensions {
			if !seen[ext] {
				seen[ext] = true
				finalExts = append(finalExts, ext)
			}
		}
	}

	if len(finalExts) == 0 && (vscode == nil || vscode["extensions"] == nil) {
		return nil
	}

	if customizations == nil {
		customizations = make(map[string]any)
		root["customizations"] = customizations
	}
	if vscode == nil {
		vscode = make(map[string]any)
		customizations["vscode"] = vscode
	}
	vscode["extensions"] = finalExts
	return nil
}

func mergeForwardPorts(root map[string]any, plan config.ResolvedPlan) error {
	var initialPorts []int
	hasBase := false
	if rawPorts, ok := root["forwardPorts"]; ok && rawPorts != nil {
		hasBase = true
		if list, ok := rawPorts.([]any); ok {
			for _, item := range list {
				switch v := item.(type) {
				case float64:
					p := int(v)
					if p <= 0 || p > 65535 {
						return fmt.Errorf("invalid base forward port %d: must be between 1 and 65535", p)
					}
					initialPorts = append(initialPorts, p)
				case int:
					if v <= 0 || v > 65535 {
						return fmt.Errorf("invalid base forward port %d: must be between 1 and 65535", v)
					}
					initialPorts = append(initialPorts, v)
				default:
					return fmt.Errorf("invalid port type %T in base forwardPorts", item)
				}
			}
		} else if list, ok := rawPorts.([]int); ok {
			for _, v := range list {
				if v <= 0 || v > 65535 {
					return fmt.Errorf("invalid base forward port %d: must be between 1 and 65535", v)
				}
				initialPorts = append(initialPorts, v)
			}
		}
	}

	seen := make(map[int]bool)
	var finalPorts []int
	for _, p := range initialPorts {
		if !seen[p] {
			seen[p] = true
			finalPorts = append(finalPorts, p)
		}
	}

	hasPluginPorts := false
	for _, plugin := range plan.Plugins {
		for _, p := range plugin.DevContainer.ForwardPorts {
			hasPluginPorts = true
			if p <= 0 || p > 65535 {
				return fmt.Errorf("plugin %q: invalid forward port %d: must be between 1 and 65535", plugin.ID, p)
			}
			if !seen[p] {
				seen[p] = true
				finalPorts = append(finalPorts, p)
			}
		}
	}

	if hasBase || hasPluginPorts {
		if finalPorts == nil {
			finalPorts = []int{}
		}
		root["forwardPorts"] = finalPorts
	}
	return nil
}

func mergeContainerEnv(root map[string]any, plan config.ResolvedPlan) error {
	env := make(map[string]string)
	hasBase := false

	if rawEnv, ok := root["containerEnv"]; ok && rawEnv != nil {
		hasBase = true
		if m, ok := rawEnv.(map[string]any); ok {
			for k, v := range m {
				env[k] = fmt.Sprint(v)
			}
		} else if m, ok := rawEnv.(map[string]string); ok {
			for k, v := range m {
				env[k] = v
			}
		}
	}

	hasPluginEnv := false
	for _, plugin := range plan.Plugins {
		for k, v := range plugin.DevContainer.ContainerEnv {
			hasPluginEnv = true
			if existingVal, ok := env[k]; ok {
				if existingVal != v {
					return fmt.Errorf("plugin %q: conflicting containerEnv for key %q: %q vs %q", plugin.ID, k, existingVal, v)
				}
			} else {
				env[k] = v
			}
		}
	}

	if hasBase || hasPluginEnv {
		root["containerEnv"] = env
	}
	return nil
}

func extractMountTarget(mount string) string {
	parts := strings.Split(mount, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if k == "target" || k == "destination" || k == "dst" {
				return v
			}
		}
	}
	return ""
}

func mergeMounts(root map[string]any, plan config.ResolvedPlan) error {
	var baseMounts []string
	hasBase := false
	if rawMounts, ok := root["mounts"]; ok && rawMounts != nil {
		hasBase = true
		if list, ok := rawMounts.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					baseMounts = append(baseMounts, s)
				}
			}
		} else if list, ok := rawMounts.([]string); ok {
			baseMounts = append(baseMounts, list...)
		}
	}

	seenExact := make(map[string]bool)
	targetToDef := make(map[string]string)
	var finalMounts []string

	for _, m := range baseMounts {
		if seenExact[m] {
			continue
		}
		seenExact[m] = true
		target := extractMountTarget(m)
		if target != "" {
			targetToDef[target] = m
		}
		finalMounts = append(finalMounts, m)
	}

	hasPluginMounts := false
	for _, plugin := range plan.Plugins {
		for _, m := range plugin.DevContainer.Mounts {
			hasPluginMounts = true
			if seenExact[m] {
				continue
			}
			target := extractMountTarget(m)
			if target != "" {
				if existingDef, ok := targetToDef[target]; ok {
					return fmt.Errorf("plugin %q: conflicting mount definitions for target %q: %q vs %q", plugin.ID, target, existingDef, m)
				}
				targetToDef[target] = m
			}
			seenExact[m] = true
			finalMounts = append(finalMounts, m)
		}
	}

	if hasBase || hasPluginMounts {
		if finalMounts == nil {
			finalMounts = []string{}
		}
		root["mounts"] = finalMounts
	}
	return nil
}

func mergePostCreateCommands(root map[string]any, plan config.ResolvedPlan) error {
	var commands []string
	hasBase := false

	if rawCmds, ok := root["postCreateCommands"]; ok && rawCmds != nil {
		hasBase = true
		if list, ok := rawCmds.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					commands = append(commands, s)
				}
			}
		} else if list, ok := rawCmds.([]string); ok {
			commands = append(commands, list...)
		}
	}

	hasPluginCmds := false
	for _, plugin := range plan.Plugins {
		for _, cmd := range plugin.DevContainer.PostCreateCommands {
			hasPluginCmds = true
			commands = append(commands, cmd)
		}
	}

	if hasBase || hasPluginCmds {
		if commands == nil {
			commands = []string{}
		}
		root["postCreateCommands"] = commands
	}
	return nil
}
