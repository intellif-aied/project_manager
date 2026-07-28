package main

import (
	"fmt"
	"io"
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
	uploadModePersonal   = "personal"
	uploadModeTeam       = "team"
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
			fmt.Fprintln(os.Stderr, "Aida 更新失败，请运行 aida update 后重试")
			return false
		}
		return true
	}

	if args[0] != "auto-sync" {
		_ = autoSyncEnsure()
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
	case "log":
		return cmdTeamSyncLog(os.Stdout)
	case "clients":
		return cmdClients()
	case "upload-client":
		if !requireUpdate() {
			return 3
		}
		return cmdUploadClient(args[1:])
	case "status":
		_ = writeAutoSyncStatus(os.Stdout)
		if !requireUpdate() {
			return 3
		}
		return cmdStatus()
	case "update":
		code, updated := cmdUpdateResult()
		if updated {
			_ = restartAutoSyncAfterUpdate()
		}
		return code
	case "version", "--version", "-v":
		fmt.Printf("aida %s\n", Version)
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	default:
		fmt.Printf("未知命令：%s\n\n", args[0])
		printUsage()
		return 2
	}
}

func printUsage() {
	writeUsage(os.Stdout)
}

func writeUsage(output io.Writer) {
	fmt.Fprint(output, `Aida CLI

登录并同步 AI 编程 Session。

用法：
  aida <命令>

常用命令：
  login                 登录 Aida
  upload                选择并上传 Session
  upload --all          上传全部 Session
  upload --team         按团队目录配置上传全部 Session
  log                   查看团队模式待配置目录
  clients               检测其他受支持客户端
  upload-client         显式选择并上传其他客户端 Session
  auto-sync enable      开启自动同步（个人模式）
  auto-sync enable --team
                        开启团队模式自动同步
  auto-sync set-time    修改同步时间
  auto-sync disable     关闭自动同步
  status                检查登录、连接和自动同步状态
  update                更新 Aida
  version               查看当前版本
  help                  查看帮助
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
