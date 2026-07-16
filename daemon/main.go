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

func saveConfig(cfg *Config) {
	data, _ := yaml.Marshal(cfg)
	os.WriteFile(configPath(), data, 0600)
}

func requireAuth(cfg *Config) {
	if cfg.Token == "" {
		fmt.Println("Not logged in. Run: aida login")
		os.Exit(1)
	}
	if cfg.APIURL == "" {
		fmt.Println("Server URL not set. Run: aida login --server <url>")
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "login":
		requireCurrentVersion(loadConfig())
		cmdLogin(os.Args[2:])
	case "sessions", "ls":
		requireCurrentVersion(loadConfig())
		cmdSessions(os.Args[2:])
	case "upload", "push":
		requireCurrentVersion(loadConfig())
		cmdUpload(os.Args[2:])
	case "status":
		requireCurrentVersion(loadConfig())
		cmdStatus()
	case "update":
		cmdUpdate()
	case "version", "--version", "-v":
		fmt.Printf("aida %s\n", Version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func requireCurrentVersion(cfg *Config) {
	if err := maybeAutoUpdate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Aida update required but failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'aida update' or reinstall Aida, then retry this command.")
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`aida - CLI for uploading AI coding sessions to Aida

Usage:
  aida <command> [options]

Commands:
  login                                      Enter your personal token interactively
  sessions [--all] [--project <dir>] [--page N] [--page-size N] [--json]
                                            List local Claude Code and Codex sessions
  upload   [numbers...] [--all] [--page-size N]
                                            Upload sessions to server
  status                                     Show current login status
  update                                     Check and install the latest version
  version                                    Show CLI version

Examples:
  # Login interactively using the service address installed with Aida
  aida login

  # List recent sessions (last 48h)
  aida sessions

  # List all sessions
  aida sessions --all

  # Show page 2 with 50 sessions per page
  aida sessions --all --page 2 --page-size 50

  # Filter by project directory
  aida sessions --project project-manager

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
