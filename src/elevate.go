//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isElevated 检查当前进程 token 是否属于管理员组且已启用
func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// selfElevate 以 runas 重新启动自身并返回（调用方随后退出）。
// 使用 ShellExecuteW 的 "runas" 动词触发 UAC。
func selfElevate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	cwd, _ := windows.UTF16PtrFromString(windowsDirectory())

	// 过滤掉 -no-elevate，避免提权后再次跳过
	var args []string
	for _, a := range os.Args[1:] {
		if !strings.EqualFold(a, "-no-elevate") && !strings.EqualFold(a, "/no-elevate") {
			args = append(args, a)
		}
	}
	params, _ := windows.UTF16PtrFromString(strings.Join(args, " "))

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	// ShellExecuteW(hwnd, verb, file, parameters, directory, showCmd)
	res, _, err := proc.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(cwd)),
		uintptr(swShowNormal),
	)
	if res <= 32 {
		return fmt.Errorf("ShellExecuteW runas failed: %w", err)
	}
	return nil
}

const swShowNormal = 1

func windowsDirectory() string {
	dir, err := windows.GetWindowsDirectory()
	if err != nil || dir == "" {
		return `C:\Windows`
	}
	return dir
}
