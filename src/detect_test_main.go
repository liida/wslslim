//go:build detecttest

package main

import (
	"fmt"
	"os"
)

// 探测逻辑离线测试入口：go build -tags detecttest
func main() {
	res := runDetect()
	fmt.Println("targets:", len(res.Targets), "reclaim:", res.DockerReclaim)
	fmt.Printf("WSL: version=%q exe=%q\n", res.WSLVersion, res.WSLExe)
	fmt.Printf("DockerDesktop: version=%q exe=%q\n", res.DockerVersion, res.DesktopExe)
	// 验证编码：wsl --list 输出解码后应无 \x00 且含可读发行版名
	out, _ := runCmdCombined("wsl.exe", "--list", "--verbose")
	fmt.Printf("WSL-OUTPUT(%d bytes, hasNUL=%v): %q\n", len(out), containsNUL(out), firstLine(out))
	// diskpart 中文输出（GBK）解码验证：diskpart 不带参数会进入交互模式，改用 cmd echo GBK 中文
	out2, err := runCmdTimeout(30*1e9, "cmd", "/c", "echo 测试中文")
	fmt.Printf("CMD-OUTPUT: %q err=%v\n", firstLine(out2), err)
	os.Exit(0)
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return s[:i]
		}
	}
	return s
}
