//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	skip   atomic.Bool
	appCtx context.Context
)

func fmtSprintf(format string, args ...interface{}) string { return fmt.Sprintf(format, args...) }

func emitEvent(ctx context.Context, name string, data ...interface{}) {
	runtime.EventsEmit(ctx, name, data...)
}

// DetectResult 探测结果
type DetectResult struct {
	Targets          []Target `json:"targets"`
	DockerReclaim    string   `json:"dockerReclaim"`    // 如 "21.46 GB"，不可用为 "—"
	DockerRunning    bool     `json:"dockerRunning"`
	OptimizeVHDAvail bool     `json:"optimizeVhdAvail"` // Optimize-VHD (Hyper-V 模块) 是否可用
	DetectedAt       string   `json:"detectedAt"`
	Error            string   `json:"error,omitempty"`
}

// Target 一个可压缩目标
type Target struct {
	Name     string `json:"name"`           // 显示名，如 "Ubuntu"
	Vhdx     string `json:"vhdx"`           // vhdx 完整路径
	Kind     string `json:"kind"`           // wsl / docker-main / docker-data / custom
	SizeBytes int64 `json:"sizeBytes"`      // os.Stat 大小；文件不存在为 -1
	SizeText string `json:"sizeText"`       // 人读大小
	Missing  bool   `json:"missing"`        // vhdx 文件不存在
	IsCustom bool   `json:"isCustom"`       // 用户手动添加
}

// runDetect 执行完整探测
func runDetect() DetectResult {
	res := DetectResult{
		DetectedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	seen := map[string]bool{}
	add := func(t Target) {
		if t.Vhdx == "" || seen[strings.ToLower(t.Vhdx)] {
			return
		}
		seen[strings.ToLower(t.Vhdx)] = true
		st, err := os.Stat(t.Vhdx)
		if err != nil {
			t.Missing = true
			t.SizeBytes = -1
			t.SizeText = "文件不存在"
		} else {
			t.SizeBytes = st.Size()
			t.SizeText = humanSize(st.Size())
		}
		res.Targets = append(res.Targets, t)
	}

	// 1. WSL 发行版（Lxss 注册表）
	for _, d := range enumLxssDistros() {
		add(Target{Name: d.Name, Vhdx: d.Vhdx, Kind: "wsl"})
	}

	// 2. Docker Desktop
	dm, dd := locateDockerVhdx()
	if dm != "" {
		add(Target{Name: "Docker 引擎盘", Vhdx: dm, Kind: "docker-main"})
	}
	if dd != "" {
		add(Target{Name: "Docker 数据盘", Vhdx: dd, Kind: "docker-data"})
	}

	// 3. docker 可回收空间
	res.DockerReclaim, res.DockerRunning = dockerReclaimable()

	// 4. Optimize-VHD 可用性（较慢，PowerShell 冷启动）
	res.OptimizeVHDAvail = optimizeVHDAvailable()

	// 5. 目标为空时给出原因提示（前端在空表处显示）
	if len(res.Targets) == 0 {
		switch {
		case lxssKeyExists():
			res.Error = "未检测到 WSL 发行版或 Docker Desktop 的 vhdx。若已安装，请先启动一次使其注册；也可在下方「＋ 添加自定义 vhdx」手动指定路径。"
		default:
			res.Error = "未检测到 WSL（注册表 Lxss 不存在）且未找到 Docker Desktop。可安装 WSL2 后重试，或通过「＋ 添加自定义 vhdx」手动指定要压缩的 vhdx 文件。"
		}
	}
	return res
}

// lxssKeyExists 判断当前用户 Lxss 注册表键是否存在（区分"没装 WSL"和"装了但没有发行版"）
func lxssKeyExists() bool {
	k, err := registryOpenKey(`Software\Microsoft\Windows\CurrentVersion\Lxss`)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

// wslSparseSupported 已废弃：WSL 对已有非稀疏 VHD 转 sparse 要求 --allow-unsafe（有数据风险），功能已移除

func humanSize(n int64) string {
	if n < 0 {
		return "—"
	}
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1024 || u == "TB" {
			return fmt.Sprintf("%.2f %s", f, u)
		}
		f /= 1024
	}
	return ""
}

// ---- Lxss 注册表枚举 ----

type lxssDistro struct {
	Name string
	Vhdx string
}

func enumLxssDistros() []lxssDistro {
	var out []lxssDistro
	root := `Software\Microsoft\Windows\CurrentVersion\Lxss`
	k, err := registryOpenKey(root)
	if err != nil {
		return out
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return out
	}
	for _, sub := range names {
		sk, err := registryOpenKey(root + `\` + sub)
		if err != nil {
			continue
		}
		name, _, _ := sk.GetStringValue("DistributionName")
		base, _, _ := sk.GetStringValue("BasePath")
		vhd, _, _ := sk.GetStringValue("VhdFileName")
		sk.Close()
		if vhd == "" {
			vhd = "ext4.vhdx"
		}
		base = strings.TrimPrefix(base, `\\?\`)
		if base == "" || strings.Contains(name, "docker-desktop") {
			continue // docker-desktop 系发行版由专用逻辑处理
		}
		out = append(out, lxssDistro{Name: name, Vhdx: filepath.Join(base, vhd)})
	}
	return out
}

// locateDockerVhdx 定位 Docker Desktop 引擎盘和数据盘（新版单发行版布局优先）
func locateDockerVhdx() (mainVhdx, dataVhdx string) {
	// 优先级 1: settings-store.json 的 CustomWslDistroDir
	dir := dockerWslDirFromSettings()
	if dir != "" {
		mainVhdx = filepath.Join(dir, "main", "ext4.vhdx")
		if fileExists(mainVhdx) {
			dataVhdx = filepath.Join(dir, "disk", "docker_data.vhdx")
			return
		}
		mainVhdx = ""
	}

	// 优先级 2: Lxss 里 docker-desktop 发行版的 BasePath
	for _, d := range enumLxssDistros() {
		_ = d
	}
	if bp, ok := lxssBasePath("docker-desktop"); ok {
		m := filepath.Join(bp, "ext4.vhdx")
		if fileExists(m) {
			mainVhdx = m
		}
		// 同级 disk\docker_data.vhdx
		dd := filepath.Join(filepath.Dir(bp), "disk", "docker_data.vhdx")
		if fileExists(dd) {
			dataVhdx = dd
		}
		// 旧版布局: docker-desktop-data 发行版
		if bp2, ok2 := lxssBasePath("docker-desktop-data"); ok2 {
			dd2 := filepath.Join(bp2, "ext4.vhdx")
			if fileExists(dd2) {
				dataVhdx = dd2
			}
		}
		if mainVhdx != "" || dataVhdx != "" {
			return
		}
	}

	// 优先级 3: %LOCALAPPDATA%\Docker\wsl 默认目录
	local := os.Getenv("LOCALAPPDATA")
	if local != "" {
		d := filepath.Join(local, "Docker", "wsl")
		m := filepath.Join(d, "main", "ext4.vhdx")
		if fileExists(m) {
			mainVhdx = m
		}
		dd := filepath.Join(d, "disk", "docker_data.vhdx")
		if fileExists(dd) {
			dataVhdx = dd
		}
	}
	return
}

func dockerWslDirFromSettings() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return ""
	}
	// 新版 settings-store.json
	if dir := readCustomDistroDir(filepath.Join(appdata, "Docker", "settings-store.json")); dir != "" {
		return dir
	}
	// 旧版 settings.json
	return readCustomDistroDir(filepath.Join(appdata, "Docker", "settings.json"))
}

func readCustomDistroDir(path string) string {
	if !fileExists(path) {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	// 简单解析 JSON 字段 CustomWslDistroDir
	var buf [65536]byte
	n, _ := f.Read(buf[:])
	s := string(buf[:n])
	key := `"CustomWslDistroDir"`
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return strings.ReplaceAll(rest[:k], `\\`, `\`)
}

func lxssBasePath(distro string) (string, bool) {
	root := `Software\Microsoft\Windows\CurrentVersion\Lxss`
	k, err := registryOpenKey(root)
	if err != nil {
		return "", false
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return "", false
	}
	for _, sub := range names {
		sk, err := registryOpenKey(root + `\` + sub)
		if err != nil {
			continue
		}
		name, _, _ := sk.GetStringValue("DistributionName")
		base, _, _ := sk.GetStringValue("BasePath")
		sk.Close()
		if strings.EqualFold(name, distro) {
			return strings.TrimPrefix(base, `\\?\`), true
		}
	}
	return "", false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// dockerReclaimable 返回 docker 可回收空间描述 + daemon 是否在运行
func dockerReclaimable() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "—", false
	}
	total := int64(-1)
	sc := bufio.NewScanner(strings.NewReader(decodeConsoleBytes(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		obj := parseJSONLight(line)
		typ := strings.ToLower(strings.Trim(`"`+obj["Type"], `"`))
		rec := strings.Trim(obj["Reclaimable"], ` "`)
		if !strings.Contains(strings.ToLower(rec), "reclaimable") {
			if b, ok := parseHumanBytes(strings.SplitN(rec, " ", 2)[0]); ok {
				if typ == "images" || typ == "local volumes" || typ == "build cache" || typ == "containers" {
					if total < 0 {
						total = 0
					}
					total += b
				}
			}
		}
	}
	if total < 0 {
		return "—", true
	}
	return humanSize(total), true
}

// parseHumanBytes 解析 "21.46GB" / "100MB" 等
func parseHumanBytes(s string) (int64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false
	}
	mult := map[byte]float64{'b': 1, 'k': 1024, 'm': 1024 * 1024, 'g': 1024 * 1024 * 1024, 't': 1024 * 1024 * 1024 * 1024}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, false
	}
	var num float64
	if _, err := fmt.Sscanf(s[:i], "%f", &num); err != nil {
		return 0, false
	}
	unit := byte('b')
	if i < len(s) {
		unit = s[i]
	}
	m, ok := mult[unit]
	if !ok {
		return 0, false
	}
	return int64(num * m), true
}

// parseJSONLight 极简 JSON 对象解析（docker --format json 单行输出）
func parseJSONLight(line string) map[string]string {
	out := map[string]string{}
	i := 0
	readStr := func() (string, bool) {
		// 从 i 处读一个 JSON 字符串（假定 i 指向起始引号）
		if i >= len(line) || line[i] != '"' {
			return "", false
		}
		i++
		start := i
		for i < len(line) {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == '"' {
				s := line[start:i]
				i++
				return s, true
			}
			i++
		}
		return "", false
	}
	skipWS := func() {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	skipWS()
	if i >= len(line) || line[i] != '{' {
		return out
	}
	i++
	for {
		skipWS()
		if i >= len(line) || line[i] == '}' {
			break
		}
		key, ok := readStr()
		if !ok {
			break
		}
		skipWS()
		if i >= len(line) || line[i] != ':' {
			break
		}
		i++
		skipWS()
		var val string
		if i < len(line) && line[i] == '"' {
			if v, ok := readStr(); ok {
				val = v
			}
		} else {
			start := i
			for i < len(line) && line[i] != ',' && line[i] != '}' {
				i++
			}
			val = strings.TrimSpace(line[start:i])
		}
		out[key] = val
		skipWS()
		if i < len(line) && line[i] == ',' {
			i++
		}
	}
	return out
}
