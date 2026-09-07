package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func contains(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func TestLoadBotConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bot.toml"), `model = "deepseek-v4-flash"
tools = ["wax", "bqx"]
`)

	bc, found, err := readBotConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("bot.toml should be found")
	}
	if bc.Model != "deepseek-v4-flash" {
		t.Fatalf("model = %q", bc.Model)
	}
	if fmtTools(bc.Tools) != "wax bqx" {
		t.Fatalf("tools = %v", bc.Tools)
	}
}

func TestLoadBotConfigMissing(t *testing.T) {
	_, found, err := readBotConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected found=false for missing bot.toml")
	}
}

func TestReadBotConfigEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bot.toml"), `base_url = "https://api.deepseek.com"
consumer = "slaxi"
[env]
WAX_NO_SANDBOX = "1"

[[pre_run]]
command = "echo hi"
`)
	bc, _, err := readBotConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if bc.Env["WAX_NO_SANDBOX"] != "1" {
		t.Fatalf("env = %v", bc.Env)
	}
	if bc.Consumer != "slaxi" {
		t.Fatalf("consumer = %q", bc.Consumer)
	}
	if len(bc.PreRun) != 1 || bc.PreRun[0].Command != "echo hi" {
		t.Fatalf("pre_run = %+v", bc.PreRun)
	}
}

func TestEnvValue(t *testing.T) {
	env := []string{"A=1", "B=two"}
	if envValue(env, "A") != "1" || envValue(env, "B") != "two" || envValue(env, "C") != "" {
		t.Fatalf("envValue wrong: %v", env)
	}
}

func TestSetEnv(t *testing.T) {
	env := []string{"A=1", "B=2"}
	env = setEnv(env, "A", "X")
	if !contains(env, "A=X") {
		t.Fatalf("replace failed: %v", env)
	}
	env = setEnv(env, "C", "3")
	if !contains(env, "C=3") {
		t.Fatalf("append failed: %v", env)
	}
}

func TestSetEnvIfUnset(t *testing.T) {
	env := []string{"A=1"}
	env = setEnvIfUnset(env, "A", "X")
	if !contains(env, "A=1") || contains(env, "A=X") {
		t.Fatalf("must not override existing: %v", env)
	}
	env = setEnvIfUnset(env, "B", "Y")
	if !contains(env, "B=Y") {
		t.Fatalf("must add missing: %v", env)
	}
}

func TestInstall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bot.toml"), `tools = ["wax", "bqx"]
`)

	bindir := filepath.Join(t.TempDir(), "bin")
	installer := filepath.Join(t.TempDir(), "install.sh")
	writeFile(t, installer, `#!/bin/sh
set -eu
bindir="${AX_PREFIX:-$HOME/.local}/bin"
mkdir -p "$bindir"
for p in "$@"; do : > "$bindir/$p"; chmod 0755 "$bindir/$p"; done
`)

	t.Setenv("AX_PREFIX", filepath.Dir(bindir))
	t.Setenv("PATH", "/usr/bin:/bin")

	if err := install(dir, "file://"+installer); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"ax", "slaxi", "wax", "bqx"} {
		if _, err := os.Stat(filepath.Join(bindir, p)); err != nil {
			t.Errorf("installer did not create %s: %v", p, err)
		}
	}
}

func TestInstallIdempotentSkipsPresent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bot.toml"), `tools = ["wax"]
`)

	bindir := t.TempDir()
	// ax and slaxi are already present; the installer must only create wax.
	for _, p := range []string{"ax", "slaxi"} {
		src := filepath.Join(bindir, p)
		writeFile(t, src, "")
		if err := os.Chmod(src, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installer := filepath.Join(t.TempDir(), "install.sh")
	writeFile(t, installer, `#!/bin/sh
set -eu
bindir="${AX_PREFIX:-$HOME/.local}/bin"
mkdir -p "$bindir"
for p in "$@"; do : > "$bindir/$p"; chmod 0755 "$bindir/$p"; done
`)
	t.Setenv("AX_PREFIX", bindir)
	t.Setenv("PATH", bindir+":/usr/bin:/bin")

	if err := install(dir, "file://"+installer); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bindir, "bin", "wax")); err != nil {
		t.Errorf("wax was not installed: %v", err)
	}
}

func fmtTools(tools []string) string { return strings.Join(tools, " ") }
