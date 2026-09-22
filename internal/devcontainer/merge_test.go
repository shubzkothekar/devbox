package devcontainer_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
	"github.com/shubzkothekar/devbox/internal/devcontainer"
)

func TestMerge_ExtensionsDeduplicationPreservingOrder(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "customizations": {
    "vscode": {
      "extensions": [
        "golang.go",
        "dbaeumer.vscode-eslint"
      ]
    }
  }
}`)

	plan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "docker",
				DevContainer: config.DevContainerContribution{
					Extensions: []string{
						"ms-azuretools.vscode-docker",
						"golang.go", // duplicate from base
					},
				},
			},
			{
				ID: "gitlens",
				DevContainer: config.DevContainerContribution{
					Extensions: []string{
						"eamodio.gitlens",
						"ms-azuretools.vscode-docker", // duplicate from earlier plugin
					},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Name           string `json:"name"`
		Customizations struct {
			VSCode struct {
				Extensions []string `json:"extensions"`
			} `json:"vscode"`
		} `json:"customizations"`
	}
	if err := json.Unmarshal(mergedBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	expected := []string{
		"golang.go",
		"dbaeumer.vscode-eslint",
		"ms-azuretools.vscode-docker",
		"eamodio.gitlens",
	}
	if len(result.Customizations.VSCode.Extensions) != len(expected) {
		t.Fatalf("expected %d extensions, got %d: %v", len(expected), len(result.Customizations.VSCode.Extensions), result.Customizations.VSCode.Extensions)
	}
	for i, ext := range expected {
		if result.Customizations.VSCode.Extensions[i] != ext {
			t.Errorf("extension[%d] = %q, expected %q", i, result.Customizations.VSCode.Extensions[i], ext)
		}
	}
}

func TestMerge_ForwardPortsDeduplication(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "forwardPorts": [3000, 8080]
}`)

	plan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					ForwardPorts: []int{8080, 5432},
				},
			},
			{
				ID: "p2",
				DevContainer: config.DevContainerContribution{
					ForwardPorts: []int{5432, 6379},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		ForwardPorts []int `json:"forwardPorts"`
	}
	if err := json.Unmarshal(mergedBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	expected := []int{3000, 8080, 5432, 6379}
	if len(result.ForwardPorts) != len(expected) {
		t.Fatalf("expected %d ports, got %d: %v", len(expected), len(result.ForwardPorts), result.ForwardPorts)
	}
	for i, port := range expected {
		if result.ForwardPorts[i] != port {
			t.Errorf("port[%d] = %d, expected %d", i, result.ForwardPorts[i], port)
		}
	}
}

func TestMerge_ForwardPortsInvalid(t *testing.T) {
	baseJSON := []byte(`{"name": "DevBox"}`)
	plan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					ForwardPorts: []int{0},
				},
			},
		},
	}

	_, err := devcontainer.Merge(baseJSON, plan)
	if err == nil {
		t.Fatal("expected error for invalid port 0, got nil")
	}
}

func TestMerge_ContainerEnvMergingAndConflict(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "containerEnv": {
    "BASE_VAR": "base_val",
    "SHARED_VAR": "same_val"
  }
}`)

	// Successful merge with identical existing value
	cleanPlan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					ContainerEnv: map[string]string{
						"SHARED_VAR": "same_val",
						"P1_VAR":     "p1_val",
					},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, cleanPlan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		ContainerEnv map[string]string `json:"containerEnv"`
	}
	if err := json.Unmarshal(mergedBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	if result.ContainerEnv["BASE_VAR"] != "base_val" ||
		result.ContainerEnv["SHARED_VAR"] != "same_val" ||
		result.ContainerEnv["P1_VAR"] != "p1_val" {
		t.Errorf("unexpected containerEnv: %+v", result.ContainerEnv)
	}

	// Conflict with differing value
	conflictPlan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p2",
				DevContainer: config.DevContainerContribution{
					ContainerEnv: map[string]string{
						"SHARED_VAR": "different_val",
					},
				},
			},
		},
	}

	_, err = devcontainer.Merge(baseJSON, conflictPlan)
	if err == nil {
		t.Fatal("expected error on conflicting containerEnv, got nil")
	}
	if !strings.Contains(err.Error(), "SHARED_VAR") {
		t.Errorf("expected error to mention SHARED_VAR, got: %v", err)
	}
}

func TestMerge_MountsMergingAndCollision(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "mounts": [
    "source=data-vol,target=/data,type=bind"
  ]
}`)

	// Clean merge: duplicate identical mount and a new mount
	cleanPlan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					Mounts: []string{
						"source=data-vol,target=/data,type=bind", // identical to base
						"source=docker-sock,target=/var/run/docker.sock,type=bind",
					},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, cleanPlan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Mounts []string `json:"mounts"`
	}
	if err := json.Unmarshal(mergedBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	if len(result.Mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d: %v", len(result.Mounts), result.Mounts)
	}
	if result.Mounts[0] != "source=data-vol,target=/data,type=bind" {
		t.Errorf("mount[0] = %q", result.Mounts[0])
	}
	if result.Mounts[1] != "source=docker-sock,target=/var/run/docker.sock,type=bind" {
		t.Errorf("mount[1] = %q", result.Mounts[1])
	}

	// Collision: same target (/var/run/docker.sock) with differing definition
	collisionPlan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					Mounts: []string{
						"source=docker-sock,target=/var/run/docker.sock,type=bind",
					},
				},
			},
			{
				ID: "p2",
				DevContainer: config.DevContainerContribution{
					Mounts: []string{
						"source=podman-sock,target=/var/run/docker.sock,type=bind",
					},
				},
			},
		},
	}

	_, err = devcontainer.Merge(baseJSON, collisionPlan)
	if err == nil {
		t.Fatal("expected error on mount target collision, got nil")
	}
	if !strings.Contains(err.Error(), "/var/run/docker.sock") {
		t.Errorf("expected error to mention target path, got: %v", err)
	}
}

func TestMerge_PostCreateCommandsOrder(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "postCreateCommands": [
    "echo base"
  ]
}`)

	plan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "docker",
				DevContainer: config.DevContainerContribution{
					PostCreateCommands: []string{"docker version"},
				},
			},
			{
				ID: "node",
				DevContainer: config.DevContainerContribution{
					PostCreateCommands: []string{"node --version", "npm --version"},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		PostCreateCommands []string `json:"postCreateCommands"`
	}
	if err := json.Unmarshal(mergedBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	expected := []string{
		"echo base",
		"docker version",
		"node --version",
		"npm --version",
	}
	if len(result.PostCreateCommands) != len(expected) {
		t.Fatalf("expected %d commands, got %d: %v", len(expected), len(result.PostCreateCommands), result.PostCreateCommands)
	}
	for i, cmd := range expected {
		if result.PostCreateCommands[i] != cmd {
			t.Errorf("postCreateCommands[%d] = %q, expected %q", i, result.PostCreateCommands[i], cmd)
		}
	}
}

func TestMerge_PreservesBaseProperties(t *testing.T) {
	baseJSON := []byte(`{
  "name": "DevBox",
  "dockerComposeFile": "../docker-compose.yml",
  "service": "devbox",
  "workspaceFolder": "/workspace",
  "shutdownAction": "stopCompose",
  "remoteUser": "devbox"
}`)

	plan := config.ResolvedPlan{
		Plugins: []config.ResolvedPlugin{
			{
				ID: "p1",
				DevContainer: config.DevContainerContribution{
					Extensions: []string{"golang.go"},
				},
			},
		},
	}

	mergedBytes, err := devcontainer.Merge(baseJSON, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(mergedBytes, &raw); err != nil {
		t.Fatalf("failed to unmarshal merged JSON: %v", err)
	}

	if raw["name"] != "DevBox" {
		t.Errorf("name = %v", raw["name"])
	}
	if raw["dockerComposeFile"] != "../docker-compose.yml" {
		t.Errorf("dockerComposeFile = %v", raw["dockerComposeFile"])
	}
	if raw["service"] != "devbox" {
		t.Errorf("service = %v", raw["service"])
	}
	if raw["workspaceFolder"] != "/workspace" {
		t.Errorf("workspaceFolder = %v", raw["workspaceFolder"])
	}
	if raw["shutdownAction"] != "stopCompose" {
		t.Errorf("shutdownAction = %v", raw["shutdownAction"])
	}
	if raw["remoteUser"] != "devbox" {
		t.Errorf("remoteUser = %v", raw["remoteUser"])
	}
}

func TestMerge_InvalidBaseJSON(t *testing.T) {
	baseJSON := []byte(`{invalid json`)
	plan := config.ResolvedPlan{}

	_, err := devcontainer.Merge(baseJSON, plan)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

