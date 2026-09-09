package grokadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inspection(config string) map[string]any {
	report := map[string]any{
		"grokVersion":    "1.0.24",
		"configSources":  map[string]any{"layers": []any{map[string]any{"role": "user", "path": config}}},
		"permissions":    map[string]any{"managedSettingsExists": false},
		"externalCompat": map[string]any{"remoteSettingsLoaded": false, "cells": []any{map[string]any{"enabled": false}}},
		"agents":         []any{map[string]any{"source": map[string]any{"type": "builtin"}}},
	}
	for _, key := range []string{"projectInstructions", "hooks", "skills", "plugins", "marketplaces", "mcpServers", "lspServers"} {
		report[key] = []any{}
	}
	return report
}

func TestInspectionRejectsConfigurationOutsideReview(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.toml")
	writeTestFile(t, config, settings)
	check := func(report map[string]any) error {
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		return checkInspection(data, config)
	}
	if err := check(inspection(config)); err != nil {
		t.Fatal(err)
	}
	t.Run("same file with different case", func(t *testing.T) {
		alias := filepath.Join(filepath.Dir(config), "CONFIG.TOML")
		if _, err := os.Stat(alias); os.IsNotExist(err) {
			t.Skip("filesystem distinguishes path case")
		} else if err != nil {
			t.Fatal(err)
		}
		if err := check(inspection(alias)); err != nil {
			t.Fatalf("same configuration file rejected: %v", err)
		}
	})
	for name, change := range map[string]func(map[string]any){
		"unsupported version": func(r map[string]any) { r["grokVersion"] = "1.0.25" },
		"machine policy": func(r map[string]any) {
			r["configSources"] = map[string]any{"layers": []any{map[string]any{"role": "user", "path": config}, map[string]any{"role": "mdm", "path": "ai.x.grok:requirements_toml_base64"}}}
		},
		"wrong path": func(r map[string]any) {
			r["configSources"] = map[string]any{"layers": []any{map[string]any{"role": "user", "path": filepath.Join(t.TempDir(), "missing")}}}
		},
		"config parse error": func(r map[string]any) {
			r["configSources"] = map[string]any{"layers": []any{map[string]any{"role": "user", "path": config, "note": "parse error"}}}
		},
		"missing layers":      func(r map[string]any) { delete(r, "configSources") },
		"missing permissions": func(r map[string]any) { delete(r, "permissions") },
		"missing agents":      func(r map[string]any) { delete(r, "agents") },
		"managed settings": func(r map[string]any) {
			r["permissions"] = map[string]any{"managedSettingsExists": true}
		},
		"compatibility enabled": func(r map[string]any) {
			r["externalCompat"] = map[string]any{"cells": []any{map[string]any{"enabled": true}}}
		},
		"missing compatibility": func(r map[string]any) { delete(r, "externalCompat") },
		"custom agent": func(r map[string]any) {
			r["agents"] = []any{map[string]any{"source": map[string]any{"type": "user"}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := inspection(config)
			change(r)
			if err := check(r); err == nil {
				t.Fatal("unexpected native configuration accepted")
			}
		})
	}
	for _, key := range []string{"projectInstructions", "hooks", "skills", "plugins", "marketplaces", "mcpServers", "lspServers"} {
		for _, value := range []any{nil, []any{"unselected"}} {
			r := inspection(config)
			r[key] = value
			if err := check(r); err == nil {
				t.Fatalf("missing or unselected %s accepted", key)
			}
		}
	}
}

func TestEnvironmentPreservesOnlyNativeAuthenticationLocation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("GROK_HOME", filepath.Join(root, "original"))
	t.Setenv("GROK_AUTH_PATH", "")
	t.Setenv("GROK_CONFIG", "unselected config")
	t.Setenv("GROK_CONFIG_PATH", "unselected-config.toml")
	t.Setenv("GROK_AUTH_PROVIDER_COMMAND", "unselected-auth-command")
	t.Setenv("GROK_PLUGIN_DIR", "unselected-plugin")
	t.Setenv("NODE_OPTIONS", "--require unselected-code")
	t.Setenv("XAI_API_KEY", "existing-native-environment-key")
	t.Setenv("GROK_AUTH", "existing-native-inline-auth")
	session := filepath.Join(root, "session")
	env, err := environment(session)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, expected := range []string{
		"HOME=" + filepath.Join(session, "home"),
		"GROK_HOME=" + filepath.Join(session, "state"),
		"GROK_AUTH_PATH=" + filepath.Join(root, "original", "auth.json"),
		"XAI_API_KEY=existing-native-environment-key",
		"GROK_AUTH=existing-native-inline-auth",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing %s", expected)
		}
	}
	if strings.Contains(joined, "unselected") {
		t.Fatal("runtime customization leaked into the environment")
	}
	// A directory cannot contain credential JSON. Building the environment must
	// still succeed: the adapter never opens or validates the credential file.
	t.Setenv("GROK_AUTH_PATH", root)
	env, err = environment(session)
	if err != nil || !strings.Contains(strings.Join(env, "\n"), "GROK_AUTH_PATH="+root) {
		t.Fatalf("native auth path override not retained: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("environment preparation wrote credential data: %v %v", entries, err)
	}
}
