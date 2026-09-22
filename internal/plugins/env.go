package plugins

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// OptionEnvVars converts a plugin ID and its resolved options map into
// deterministic environment variable assignments in the format
// DEVBOX_PLUGIN_<UPPERCASE_PLUGIN_ID>_<UPPERCASE_OPTION>=<value>.
func OptionEnvVars(pluginID string, options map[string]any) []string {
	var keys []string
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	prefix := fmt.Sprintf("DEVBOX_PLUGIN_%s_", NormalizeEnvName(pluginID))
	for _, k := range keys {
		varName := prefix + NormalizeEnvName(k)
		valStr := FormatOptionValue(options[k])
		env = append(env, fmt.Sprintf("%s=%s", varName, valStr))
	}
	return env
}

// NormalizeEnvName converts a string to uppercase and replaces any non-alphanumeric
// characters with underscores.
func NormalizeEnvName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.ToUpper(b.String())
}

// FormatOptionValue formats an option value for shell/environment consumption.
func FormatOptionValue(val any) string {
	switch v := val.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}
