//go:build windows

package main

import "syscall"

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW（x/sys/windows 常量，syscall 包无）

func hideSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:   true,
		CreationFlags: createNoWindow, // 彻底不创建控制台窗口（执行中弹黑框的根因）
	}
}
