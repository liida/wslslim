//go:build windows

package main

import (
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// decodeConsoleBytes 把子进程输出统一转为 UTF-8：
// 纯 ASCII / 合法 UTF-8 原样返回；wsl.exe 的 UTF-16LE 输出按 UTF-16 解码；
// 其余（diskpart/taskkill/net 等中文控制台程序，GBK 编码）按 GB18030 解码
func decodeConsoleBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if looksUTF16LE(b) {
		return decodeUTF16LE(b)
	}
	if !utf8.Valid(b) {
		if s, err := simplifiedchinese.GB18030.NewDecoder().String(string(b)); err == nil && s != "" {
			return s
		}
	}
	return string(b)
}

// looksUTF16LE 检测 UTF-16LE（ASCII 字符的高位字节大量为 0）
func looksUTF16LE(b []byte) bool {
	if len(b) < 2 || len(b)%2 != 0 {
		return false
	}
	odd, oddZero := 0, 0
	for i := 1; i < len(b); i += 2 {
		odd++
		if b[i] == 0 {
			oddZero++
		}
	}
	return odd > 0 && oddZero*4 >= odd*3
}

func decodeUTF16LE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// encodeANSI 把 diskpart 脚本内容编码为 ANSI/GBK（diskpart /s 按 ANSI 读取脚本）
func encodeANSI(s string) []byte {
	b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return b
}
