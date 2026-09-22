package plugins

import (
	"reflect"
	"testing"
)

func TestOptionEnvVars(t *testing.T) {
	opts := map[string]any{
		"compose": true,
		"port":    8080,
		"name":    "dev",
	}
	got := OptionEnvVars("docker", opts)
	want := []string{
		"DEVBOX_PLUGIN_DOCKER_COMPOSE=true",
		"DEVBOX_PLUGIN_DOCKER_NAME=dev",
		"DEVBOX_PLUGIN_DOCKER_PORT=8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OptionEnvVars() = %#v, want %#v", got, want)
	}
}

func TestNormalizeEnvName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"docker", "DOCKER"},
		{"my-plugin", "MY_PLUGIN"},
		{"api.url", "API_URL"},
		{"foo_bar-baz.123", "FOO_BAR_BAZ_123"},
	}
	for _, tt := range tests {
		if got := NormalizeEnvName(tt.in); got != tt.want {
			t.Errorf("NormalizeEnvName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
