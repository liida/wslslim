//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CleanOptions 前端传入的清理选项
type CleanOptions struct {
	Targets      []TargetCfg `json:"targets"`
	PruneBefore  bool        `json:"pruneBefore"`
	PruneAll     bool        `json:"pruneAll"`
	StopDesktop  bool        `json:"stopDesktop"`
	RestartAfter bool        `json:"restartAfter"`
	DesktopExe   string      `json:"desktopExe"`
	Method       string      `json:"method"` // diskpart / optimize-vhd
}

type cleanResult struct {
	Name       string `json:"name"`
	BeforeText string `json:"beforeText"`
	AfterText  string `json:"afterText"`
	FreedText  string `json:"freedText"`
	OK         bool   `json:"ok"`
	freedBytes int64
}

var cleanRunning = make(chan struct{}, 1) // 防并发

func startClean(ctx context.Context, opts CleanOptions) bool {
	appCtx = ctx
	select {
	case cleanRunning <- struct{}{}:
	default:
		logf("已有清理任务在运行")
		return false
	}
	go func() {
		defer func() { <-cleanRunning }()
		runClean(ctx, opts)
	}()
	return true
}

func runClean(ctx context.Context, opts CleanOptions) {
	skip.Store(false)
	start := time.Now()
	logf("===== 清理开始 %s =====", start.Format("2006-01-02 15:04:05"))
	defer func() {
		logf("===== 清理结束，耗时 %.1fs =====", time.Since(start).Seconds())
	}()

	// 预统计
	type item struct {
		cfg  TargetCfg
		path string
	}
	var items []item
	for _, t := range opts.Targets {
		if !t.Enabled || !fileExists(t.Vhdx) {
			continue
		}
		items = append(items, item{cfg: t, path: t.Vhdx})
	}
	if len(items) == 0 && !opts.PruneBefore {
		logf("没有启用的目标，退出")
		return
	}

	hasDocker := false
	for _, it := range items {
		if it.cfg.Kind == "docker-main" || it.cfg.Kind == "docker-data" {
			hasDocker = true
		}
	}

	// 步骤 1: docker prune
	if opts.PruneBefore {
		if skipRequested() {
			logf("[跳过] docker prune")
		} else {
			stepPrune(opts)
		}
	}

	// 步骤 2: 关闭 Docker Desktop
	if opts.StopDesktop && hasDocker && !skipRequested() {
		stepStopDocker()
	}

	// 步骤 3: wsl --shutdown
	if len(items) > 0 && !skipRequested() {
		stepWslShutdown()
	}

	// 步骤 4: 压缩
	var results []cleanResult
	totalFreed := int64(0)
	for _, it := range items {
		if skipRequested() {
			logf("[跳过] %s", it.cfg.Name)
			continue
		}
		r := compactOne(ctx, it.cfg, it.path, opts)
		results = append(results, r)
		if r.OK {
			totalFreed += r.freedBytes
		}
	}

	// 步骤 5: 重启 Docker Desktop
	if opts.RestartAfter && hasDocker && !skipRequested() {
		stepRestartDocker(opts.DesktopExe)
	}

	// 步骤 6: 报告
	logf("----- 结果 -----")
	for _, r := range results {
		status := "失败"
		if r.OK {
			status = "完成"
		}
		logf("%s: %s → %s (%s, %s)", r.Name, r.BeforeText, r.AfterText, r.FreedText, status)
	}
	logf("总计释放: %s", humanSize(totalFreed))
	emitEvent(ctx, "done", map[string]interface{}{
		"results": results,
		"total":   humanSize(totalFreed),
	})
}

func skipRequested() bool { return skip.Load() }

// stepPrune docker system prune
func stepPrune(opts CleanOptions) {
	if !dockerDaemonUp() {
		logf("[1/6] docker daemon 未运行，跳过 prune")
		return
	}
	logf("[1/6] docker prune ...")
	args := []string{"system", "prune", "-f"}
	if opts.PruneAll {
		args = append(args, "-a")
	}
	args = append(args, "--volumes")
	out, err := runCmdCombined("docker", args...)
	if err != nil {
		logf("docker prune 失败: %v\n%s", err, out)
	} else {
		logf("docker prune 完成:\n%s", strings.TrimSpace(out))
	}
}

func dockerDaemonUp() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	hideWindow(cmd)
	return cmd.Run() == nil
}

// stepStopDocker 关闭 Docker Desktop
func stepStopDocker() {
	logf("[2/6] 关闭 Docker Desktop ...")
	for _, proc := range []string{"Docker Desktop", "com.docker.backend", "com.docker.dev-envs", "com.docker.build"} {
		taskkill(proc)
	}
	// 停止服务
	stopService("com.docker.service")
	time.Sleep(2 * time.Second)
}

func taskkill(proc string) {
	if out, err := runCmdCombined("taskkill", "/IM", proc+".exe", "/F"); err == nil {
		logf("  已结束 %s", proc)
	} else {
		_ = out
	}
}

func stopService(name string) {
	if out, err := runCmdCombined("net", "stop", name); err == nil {
		logf("  已停止服务 %s", name)
	} else {
		_ = out
	}
}

// stepWslShutdown 关闭 WSL 并等待句柄释放
func stepWslShutdown() {
	logf("[3/6] wsl --shutdown ...")
	out, err := runCmdCombined("wsl.exe", "--shutdown")
	if err != nil {
		logf("  wsl --shutdown: %v %s", err, out)
	}
	// 轮询等待（最多 ~30s）：wsl --list --running 输出无发行版名
	for i := 0; i < 15; i++ {
		time.Sleep(2 * time.Second)
		out, err := runCmdCombined("wsl.exe", "--list", "--running")
		t := strings.ToLower(out)
		// 正常输出包含 NUL 清理后的发行版名；无发行版时 wsl 返回非 0 或输出提示文字
		hasDistro := strings.Contains(t, "docker") || strings.Contains(t, "ubuntu") ||
			strings.Contains(t, "debian") || strings.Contains(t, "kali") ||
			strings.Contains(t, "suse") || strings.Contains(t, "arch")
		if err != nil || !hasDistro {
			logf("  WSL 已完全关闭（%.0fs）", float64(i+1)*2)
			return
		}
	}
	logf("  等待超时，继续执行")
}

// compactOne 压缩单个 vhdx
func compactOne(ctx context.Context, cfg TargetCfg, path string, opts CleanOptions) cleanResult {
	st, err := os.Stat(path)
	if err != nil {
		return cleanResult{Name: cfg.Name, BeforeText: "?", AfterText: "文件不可访问", FreedText: "—", OK: false}
	}
	before := st.Size()
	logf("[4/6] 压缩 %s (%s)", cfg.Name, path)
	logf("  压缩前: %s", humanSize(before))

	// 重试循环：文件可能仍被占用
	const maxRetries = 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if skipRequested() {
			return cleanResult{Name: cfg.Name, BeforeText: humanSize(before), AfterText: "跳过", FreedText: "—", OK: false}
		}
		if attempt > 1 {
			logf("  等待 5s 后重试 %d/%d ...", attempt-1, maxRetries-1)
			time.Sleep(5 * time.Second)
		}
		var err error
		if opts.Method == "optimize-vhd" {
			err = compactOptimizeVHD(path)
		} else {
			err = compactDiskpart(path)
		}
		if err == nil {
			st2, _ := os.Stat(path)
			after := st2.Size()
			freed := int64(0)
			if before > after {
				freed = before - after
			}
			logf("  压缩后: %s（释放 %s）", humanSize(after), humanSize(freed))
			return cleanResult{Name: cfg.Name, BeforeText: humanSize(before), AfterText: humanSize(after), FreedText: humanSize(freed), OK: true, freedBytes: freed}
		}
		logf("  尝试 %d 失败: %v", attempt, err)
		// 非"被占用"类错误不重试
		if !isBusyError(err) {
			break
		}
	}
	return cleanResult{Name: cfg.Name, BeforeText: humanSize(before), AfterText: humanSize(before), FreedText: "—", OK: false}
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "being used") || strings.Contains(s, "attached") ||
		strings.Contains(s, "access is denied") || strings.Contains(s, "denied")
}

// compactDiskpart 用 diskpart 压缩 vhdx
func compactDiskpart(path string) error {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("wslslim_%d.txt", time.Now().UnixNano()))
	// diskpart /s 按 ANSI(GBK) 读取脚本，必须编码后写入
	script := fmt.Sprintf("select vdisk file=\"%s\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\nexit\r\n", path)
	if err := os.WriteFile(tmp, encodeANSI(script), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)

	logf("  [diskpart] select vdisk → attach readonly → compact → detach")
	out, err := runCmdTimeout(60*time.Minute, "diskpart", "/s", tmp)
	logf("  [diskpart 输出]\n%s", strings.TrimSpace(out))
	if err != nil {
		return fmt.Errorf("diskpart: %w", err)
	}
	return nil
}

// compactOptimizeVHD 用 PowerShell Hyper-V 模块压缩
func compactOptimizeVHD(path string) error {
	logf("  [Optimize-VHD] %s", path)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Optimize-VHD -Path '%s' -Mode Full", strings.ReplaceAll(path, "'", "''")))
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		logf("  [Optimize-VHD 输出] %s", strings.TrimSpace(decodeConsoleBytes(out)))
	}
	return err
}

// stepRestartDocker 重启 Docker Desktop
func stepRestartDocker(exe string) {
	if exe == "" || !fileExists(exe) {
		logf("[5/6] Docker Desktop exe 不存在（%s），跳过重启", exe)
		return
	}
	logf("[5/6] 重启 Docker Desktop ...")
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		logf("  启动失败: %v", err)
	} else {
		logf("  已启动 Docker Desktop")
	}
}

// runCmdCombined 执行命令并返回 CombinedOutput（20 分钟超时，输出已转 UTF-8，无控制台弹框）
func runCmdCombined(name string, args ...string) (string, error) {
	return runCmdTimeout(20*time.Minute, name, args...)
}

// runCmdTimeout 执行命令，自定义超时（输出已转 UTF-8，无控制台弹框）
func runCmdTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return decodeConsoleBytes(out), err
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = hideSysProcAttr()
}

// optimizeVHDAvailable 检测 Hyper-V PowerShell 模块（Optimize-VHD）是否可用
func optimizeVHDAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"if (Get-Command Optimize-VHD -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }")
	hideWindow(cmd)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}
