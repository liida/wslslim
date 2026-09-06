package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 绑定给前端的对象。JS 侧通过 window.go.main.App.<方法>(...) 调用。
type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	appCtx = ctx
	initLogFile()
}

// initLogFile GUI 模式下 stdout 不可见，日志同时写入文件
func initLogFile() {
	if logFile != nil {
		return
	}
	dir := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "wslslim.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logFile = f
	log.SetOutput(io.MultiWriter(f))
	log.SetFlags(0)
	log.Printf("===== WSLSlim 启动 %s =====", time.Now().Format("2006-01-02 15:04:05"))
}

var logFile *os.File

// Detect 全量探测：WSL 发行版 + Docker Desktop vhdx + docker 可回收空间
func (a *App) Detect() DetectResult {
	return runDetect()
}

// StartClean 启动清理编排器（goroutine），立即返回，进度通过 "log" 事件推送
func (a *App) StartClean(opts CleanOptions) bool {
	return startClean(a.ctx, opts)
}

// SkipRest 置"跳过剩余步骤"标志
func (a *App) SkipRest() {
	skip.Store(true)
}

// GetConfig 读取当前配置
func (a *App) GetConfig() Config {
	return loadConfig()
}

// SaveConfig 保存配置
func (a *App) SaveConfig(cfg Config) bool {
	return saveConfig(cfg)
}

// IsElevated 当前进程是否已提权
func (a *App) IsElevated() bool {
	return isElevated()
}

// StatVhdx 获取单个 vhdx 文件信息（自定义目标添加时用）
func (a *App) StatVhdx(path string) Target {
	t := Target{Name: "", Vhdx: path, Kind: "custom", SizeBytes: -1}
	st, err := os.Stat(path)
	if err != nil {
		t.Missing = true
		t.SizeText = "文件不存在"
	} else {
		t.SizeBytes = st.Size()
		t.SizeText = humanSize(st.Size())
	}
	return t
}

// PickVhdx 弹出系统文件选择框选一个 vhdx；取消返回 ""
func (a *App) PickVhdx() string {
	if a.ctx == nil {
		return ""
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择 vhdx 虚拟磁盘文件",
		Filters: []wruntime.FileFilter{
			{DisplayName: "虚拟磁盘 (*.vhdx;*.vhd)", Pattern: "*.vhdx;*.vhd"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		logf("文件选择框错误: %v", err)
		return ""
	}
	return path
}

// logf 输出日志：推送到前端 "log" 事件
func logf(format string, args ...interface{}) {
	msg := fmtSprintf(format, args...)
	log.Print(msg)
	if appCtx != nil {
		emitEvent(appCtx, "log", msg)
	}
}

// exitApp 退出进程（os.Exit 的包装，便于将来扩展）
func exitApp(code int) {
	os.Exit(code)
}
