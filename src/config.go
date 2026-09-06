package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config 持久化配置（%APPDATA%\wslslim\config.json）
type Config struct {
	Targets []TargetCfg `json:"targets"`
	Docker  DockerCfg   `json:"docker"`
	// CompactMethod: "diskpart" 或 "optimize-vhd"
	CompactMethod string `json:"compactMethod"`
}

type TargetCfg struct {
	Name    string `json:"name"`
	Vhdx    string `json:"vhdx"`
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // wsl / docker-main / docker-data / custom
}

type DockerCfg struct {
	MainVhdx     string `json:"mainVhdx"`
	DataVhdx     string `json:"dataVhdx"`
	Enabled      bool   `json:"enabled"`
	PruneBefore  bool   `json:"pruneBefore"`
	PruneAll     bool   `json:"pruneAll"`
	StopDesktop  bool   `json:"stopDesktop"`
	RestartAfter bool   `json:"restartAfter"`
	DesktopExe   string `json:"desktopExe"`
}

func defaultConfig() Config {
	return Config{
		Targets: nil,
		Docker: DockerCfg{
			Enabled:      true,
			PruneBefore:  true,
			StopDesktop:  true,
			RestartAfter: true,
			DesktopExe:    `C:\Program Files\Docker\Docker\Docker Desktop.exe`,
		},
		CompactMethod: "diskpart",
	}
}

func configDir() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = "."
	}
	return filepath.Join(appdata, "wslslim")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	if cfg.CompactMethod != "optimize-vhd" {
		cfg.CompactMethod = "diskpart"
	}
	if cfg.Docker.DesktopExe == "" {
		cfg.Docker.DesktopExe = `C:\Program Files\Docker\Docker\Docker Desktop.exe`
	}
	return cfg
}

func saveConfig(cfg Config) bool {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return false
	}
	if cfg.CompactMethod != "optimize-vhd" {
		cfg.CompactMethod = "diskpart"
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false
	}
	return os.WriteFile(configPath(), b, 0o644) == nil
}
