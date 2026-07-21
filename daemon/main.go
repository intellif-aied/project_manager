package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	Version           = "dev"
	DefaultReleaseURL = ""
)

type Config struct {
	APIURL           string `yaml:"api_url"`
	InternalAPIURL   string `yaml:"internal_api_url,omitempty"`
	AutoRoute        bool   `yaml:"auto_route,omitempty"`
	ReleaseURL       string `yaml:"release_url,omitempty"`
	AutoUpdate       bool   `yaml:"auto_update,omitempty"`
	LastUpdateCheck  string `yaml:"last_update_check,omitempty"`
	LastKnownVersion string `yaml:"last_known_version,omitempty"`
	Token            string `yaml:"token"`
	ServerInfo       string `yaml:"server_info,omitempty"`
	ActiveAPIURL     string `yaml:"-"`
	ActiveRoute      string `yaml:"-"`
	apiURLOverridden bool
}

const (
	configFileName       = ".aida.yaml"
	legacyConfigFileName = ".aidashboard.yaml"
)

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configFileName)
}

func legacyConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, legacyConfigFileName)
}

func loadConfig() *Config {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		data, err = os.ReadFile(legacyConfigPath())
	}
	if err != nil {
		return configFromEnv(&Config{})
	}
	var cfg Config
	yaml.Unmarshal(data, &cfg)
	return configFromEnv(&cfg)
}

func configFromEnv(cfg *Config) *Config {
	if v := os.Getenv("AIDA_API_URL"); v != "" {
		cfg.APIURL = strings.TrimRight(v, "/")
		cfg.apiURLOverridden = true
	} else if v := os.Getenv("AIDASHBOARD_API_URL"); v != "" {
		cfg.APIURL = strings.TrimRight(v, "/")
		cfg.apiURLOverridden = true
	}
	if v := os.Getenv("AIDA_INTERNAL_API_URL"); v != "" {
		cfg.InternalAPIURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("AIDA_RELEASE_URL"); v != "" {
		cfg.ReleaseURL = strings.TrimRight(v, "/")
	} else if strings.TrimSpace(cfg.ReleaseURL) == "" && strings.TrimSpace(DefaultReleaseURL) != "" {
		cfg.ReleaseURL = strings.TrimRight(strings.TrimSpace(DefaultReleaseURL), "/")
	}
	if v := strings.TrimSpace(os.Getenv("AIDA_AUTO_ROUTE")); v != "" {
		cfg.AutoRoute = v == "1" || strings.EqualFold(v, "true")
	}
	if v := strings.TrimSpace(os.Getenv("AIDA_AUTO_UPDATE")); v != "" {
		cfg.AutoUpdate = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("AIDA_TOKEN"); v != "" {
		cfg.Token = v
	} else if v := os.Getenv("AIDASHBOARD_TOKEN"); v != "" {
		cfg.Token = v
	}
	return cfg
}

func saveConfig(cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func requireAuth(cfg *Config) error {
	if cfg.Token == "" {
		return fmt.Errorf("not logged in; run: aida login")
	}
	if cfg.APIURL == "" {
		return fmt.Errorf("server URL not set; run: aida login")
	}
	return nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func stdinIsInteractive() bool {
	inputInfo, err := os.Stdin.Stat()
	return err == nil && inputInfo.Mode()&os.ModeCharDevice != 0
}

func run(args []string) int {
	if len(args) < 1 {
		printUsage()
		return 0
	}

	requireUpdate := func() bool {
		if err := maybeAutoUpdate(loadConfig()); err != nil {
			fmt.Fprintf(os.Stderr, "Aida update required but failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "Run 'aida update' or reinstall Aida, then retry this command.")
			return false
		}
		return true
	}

	if args[0] != "auto-sync" {
		if err := ensureAutoSyncBackground(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: automatic Session sync background check failed: %v\n", err)
		}
	}

	switch args[0] {
	case "auto-sync":
		return cmdAutoSyncCLI(args[1:], os.Stdin, os.Stdout, stdinIsInteractive())
	case "login":
		if !requireUpdate() {
			return 3
		}
		code := cmdLogin(args[1:])
		if code == 0 {
			finishLoginAutoSync(os.Stdin, os.Stdout, stdinIsInteractive())
		}
		return code
	case "sessions", "ls":
		fmt.Fprintln(os.Stderr, "aida sessions 已移除，请使用 aida upload 查看并选择本地 Session。")
		return 2
	case "upload", "push":
		if !requireUpdate() {
			return 3
		}
		return cmdUpload(args[1:])
	case "status":
		_ = writeAutoSyncStatus(os.Stdout)
		if !requireUpdate() {
			return 3
		}
		return cmdStatus()
	case "update":
		code, updated := cmdUpdateResult()
		if updated {
			if err := restartAutoSyncAfterUpdate(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Aida updated but automatic Session sync background restart failed: %v\n", err)
			}
		}
		return code
	case "version", "--version", "-v":
		fmt.Printf("aida %s\n", Version)
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	default:
		fmt.Printf("Unknown command: %s\n\n", args[0])
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Print(`aida - CLI for uploading AI coding sessions to Aida

Usage:
  aida <command> [options]

Commands:
  login                                      Enter your personal token interactively
  auto-sync <enable|status|set-time|disable> Manage automatic Session sync
  upload   [numbers...] [--all] [--page-size N]
                                            Upload sessions to server
  status                                     Show current login status
  update                                     Check and install the latest version
  version                                    Show CLI version

Examples:
  # Login interactively using the service address installed with Aida
  aida login

  # Upload specific sessions by number
  aida upload 1 3 5

  # Upload all recent sessions
  aida upload --all

  # Interactive upload (supports paging and cross-page selection)
  aida upload

  # Check login status
  aida status

Session logs location:
  ~/.claude/projects/
  ~/.codex/sessions/

Documentation:
  PRD:        See PRD.md in the project repository
  Prototype:  See prototype.html in the project repository
  API:        http://<server>:18090/health  (health check)
`)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- status ----
