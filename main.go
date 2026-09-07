// Command boti is the generic Botdir host loader for the AX ecosystem.
//
// It reads a bot's bot.toml, installs the consumer, engine, and declared tool
// binaries, wires the standard Botdir environment, runs the bot's [[pre_run]]
// hooks, then starts the declared consumer (default slaxi).
//
// Modes:
//
//	boti install   read Root/bot.toml, install any missing tool binaries
//	boti           (run) set up the bot root and exec the consumer
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

const defaultInstallURL = "https://ax.3lines.studio/install.sh"

// botConfig holds the bot.toml root keys the loader reads. A host loader owns
// the env, pre_run, and consumer keys; consumers ignore tables they do not own.
type botConfig struct {
	Model    string            `toml:"model"`
	BaseURL  string            `toml:"base_url"`
	Tools    []string          `toml:"tools"`
	Consumer string            `toml:"consumer"`
	Env      map[string]string `toml:"env"`
	PreRun   []preRunHook      `toml:"pre_run"`
}

// preRunHook is one command run before the consumer starts.
type preRunHook struct {
	Command string `toml:"command"`
}

func main() {
	fs := flag.NewFlagSet("boti", flag.ExitOnError)
	dir := fs.String("C", "", "bot root directory (default: $BOT_ROOT, else cwd)")
	installURL := fs.String("install-url", "", "installer script URL (default: $BOTI_INSTALL_URL, else the hosted installer)")
	fs.Parse(os.Args[1:])

	root := resolveRoot(*dir)
	mode := "run"
	if args := fs.Args(); len(args) > 0 {
		mode = args[0]
	}

	switch mode {
	case "install":
		if err := install(root, installerURL(*installURL)); err != nil {
			fatal("install: %v", err)
		}
	case "run":
		if err := run(root); err != nil {
			fatal("run: %v", err)
		}
	default:
		fatal("unknown mode %q (want install or run)", mode)
	}
}

func resolveRoot(dir string) string {
	root := dir
	if root == "" {
		root = os.Getenv("BOT_ROOT")
	}
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			fatal("working directory: %v", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fatal("bot root: %v", err)
	}
	return abs
}

func installerURL(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("BOTI_INSTALL_URL"); v != "" {
		return v
	}
	return defaultInstallURL
}

// install reads Root/bot.toml and installs ax, slaxi, and each declared tool
// that is not already on PATH.
func install(root, installURL string) error {
	bc, found, err := readBotConfig(root)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("bot.toml is required in %s", root)
	}

	pkgs := []string{"ax", "slaxi"}
	seen := map[string]bool{"ax": true, "slaxi": true}
	for _, t := range bc.Tools {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		pkgs = append(pkgs, t)
	}

	var missing []string
	for _, p := range pkgs {
		if _, err := exec.LookPath(p); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	installer, err := fetchInstaller(installURL)
	if err != nil {
		return err
	}
	defer os.Remove(installer)

	for _, p := range missing {
		fmt.Fprintf(os.Stderr, "boti: installing %s\n", p)
		if err := runInstaller(installer, p); err != nil {
			return fmt.Errorf("install %s: %w", p, err)
		}
	}
	return nil
}

// run wires the standard Botdir environment, runs the bot's [[pre_run]] hooks,
// then replaces this process with the declared consumer (default slaxi).
func run(root string) error {
	// Start everything from the bot root.
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("chdir %s: %w", root, err)
	}

	env := os.Environ()
	// BOT_ROOT is the single canonical anchor: consumers and tools derive the
	// standard paths (workspace/, skills/, state/, run/, secrets/, bot.md) from
	// it by convention instead of each getting its own environment variable.
	env = setEnv(env, "BOT_ROOT", root)

	// Declarative runtime environment from the bot's [env] table. Host
	// environment wins.
	bc, _, err := readBotConfig(root)
	if err != nil {
		return err
	}
	for k, v := range bc.Env {
		env = setEnvIfUnset(env, k, v)
	}
	// Pre-start hooks run with the bot environment in order. A failing hook
	// aborts startup; a hook that may fail should handle it (e.g. `|| true`).
	for _, h := range bc.PreRun {
		if h.Command == "" {
			continue
		}
		cmd := exec.Command("/bin/sh", "-c", h.Command)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pre_run %q: %w", h.Command, err)
		}
	}

	consumer := bc.Consumer
	if consumer == "" {
		consumer = "slaxi"
	}
	consumerPath, err := exec.LookPath(consumer)
	if err != nil {
		return fmt.Errorf("find consumer %s: %w", consumer, err)
	}
	return syscall.Exec(consumerPath, []string{consumer}, env)
}

// readBotConfig reads Root/bot.toml if present. found is false and err is nil
// when the file is absent (runtime bots may omit it); a parse error is fatal.
func readBotConfig(root string) (botConfig, bool, error) {
	var bc botConfig
	data, err := os.ReadFile(filepath.Join(root, "bot.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return bc, false, nil
		}
		return bc, false, fmt.Errorf("read bot.toml: %w", err)
	}
	if err := toml.Unmarshal(data, &bc); err != nil {
		return bc, true, fmt.Errorf("parse bot.toml: %w", err)
	}
	return bc, true, nil
}

// fetchInstaller writes the installer script to a temp file and returns its
// path. It supports http(s) URLs and file:// URLs (for local testing).
func fetchInstaller(url string) (string, error) {
	var data []byte
	switch {
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		resp, err := http.Get(url) //nolint:gosec // URL comes from the host or a fixed default.
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("installer %s: %s", url, resp.Status)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
	case strings.HasPrefix(url, "file://"):
		d, err := os.ReadFile(strings.TrimPrefix(url, "file://"))
		if err != nil {
			return "", err
		}
		data = d
	default:
		return "", fmt.Errorf("unsupported installer URL %q", url)
	}

	f, err := os.CreateTemp("", "boti-install-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func runInstaller(script, pkg string) error {
	cmd := exec.Command("sh", script, pkg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// setEnv sets key=value in env, replacing any existing entry.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// setEnvIfUnset sets key=value in env only when key is not already present.
func setEnvIfUnset(env []string, key, value string) []string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return env
		}
	}
	return append(env, prefix+value)
}

// envValue returns the value of key in env, or "".
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "boti: "+format+"\n", args...)
	os.Exit(1)
}
