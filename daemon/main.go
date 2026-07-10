package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var Version = "dev"

type Config struct {
	APIURL     string `yaml:"api_url"`
	Token      string `yaml:"token"`
	ServerInfo string `yaml:"server_info,omitempty"`
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
	} else if v := os.Getenv("AIDASHBOARD_API_URL"); v != "" {
		cfg.APIURL = strings.TrimRight(v, "/")
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
		cmdLogin(os.Args[2:])
	case "sessions", "ls":
		cmdSessions(os.Args[2:])
	case "upload", "push":
		cmdUpload(os.Args[2:])
	case "status":
		cmdStatus()
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

func printUsage() {
	fmt.Print(`aida - CLI for uploading Claude Code sessions to Aida

Usage:
  aida <command> [options]

Commands:
  login    --server <url> --token <token>   Login with server URL and API token
  sessions [--all] [--project <dir>]        List local Claude Code sessions
  upload   [numbers...] [--all]             Upload sessions to server
  status                                     Show current login status
  version                                    Show CLI version

Examples:
  # Login with platform token
  aida login --server http://<server>:18090/api/v1 --token eyJhbG...

  # Login interactively (enter token when prompted)
  aida login --server http://<server>:18090/api/v1

  # List recent sessions (last 48h)
  aida sessions

  # List all sessions
  aida sessions --all

  # Filter by project directory
  aida sessions --project project-manager

  # Upload specific sessions by number
  aida upload 1 3 5

  # Upload all recent sessions
  aida upload --all

  # Interactive upload (shows picker)
  aida upload

  # Check login status
  aida status

Session logs location:
  ~/.claude/projects/

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
