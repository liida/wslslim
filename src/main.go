//go:build !detecttest

package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 捕获一切 panic 输出到日志文件（排查窗口创建静默退出）
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("PANIC: %v\n%s", r, debug.Stack())
			os.Stderr.WriteString(msg)
			if d := os.Getenv("APPDATA"); d != "" {
				_ = os.WriteFile(filepath.Join(d, "wslslim", "panic.log"), []byte(msg), 0o644)
			}
			log.Fatal(msg)
		}
	}()

	// 自提权：未提权且未指定 -no-elevate 时，runas 重启自身后退出
	if !isElevated() && !noElevateFlag() {
		if err := selfElevate(); err != nil {
			log.Fatalf("需要管理员权限，且自提权失败: %v", err)
		}
		return
	}

	app := NewApp()

	// 注意：必须 -tags production 构建（wails v2 要求，否则只弹错误框）
	err := wails.Run(&options.App{
		Title:     "WSLSlim",
		Width:     860,
		Height:    700,
		MinWidth:  760,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 249, B: 250, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "wslslim-3f8b2a91-single-instance",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				log.Println("second instance launched, ignoring")
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func noElevateFlag() bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, "-no-elevate") || strings.EqualFold(a, "/no-elevate") {
			return true
		}
	}
	return false
}
