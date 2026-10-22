package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// Цвета окна не зависят от темы Windows. По умолчанию тёмная тема; светлую
// включает интерфейс (WindowSetLightTheme), если администратор её выбрал.
var (
	windowBackground = &options.RGBA{R: 30, G: 30, B: 32, A: 255}
	titleBar         = windows.RGB(30, 30, 32)
	titleText        = windows.RGB(242, 242, 244)
	titleTextDim     = windows.RGB(154, 154, 161)

	lightTitleBar     = windows.RGB(245, 245, 247)
	lightTitleText    = windows.RGB(29, 29, 31)
	lightTitleTextDim = windows.RGB(110, 110, 115)
)

// themeFile — выбранная тема: окно создаётся сразу в ней, без тёмной вспышки
// перед светлой. Сам интерфейс берёт тему из своего хранилища.
func themeFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "FileSearchAdmin", "theme")
}

func savedTheme() string {
	if b, err := os.ReadFile(themeFile()); err == nil && strings.TrimSpace(string(b)) == "light" {
		return "light"
	}
	return "dark"
}

func main() {
	app := NewApp()
	background, theme := windowBackground, windows.Dark
	if savedTheme() == "light" {
		background, theme = &options.RGBA{R: 245, G: 245, B: 247, A: 255}, windows.Light
	}

	err := wails.Run(&options.App{
		Title:            "Удалённое управление 1С — Администрирование",
		Width:            1180,
		Height:           740,
		MinWidth:         760,
		MinHeight:        480,
		BackgroundColour: background,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind:      []any{app},
		Windows: &windows.Options{
			Theme: theme,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:           titleBar,
				DarkModeTitleBarInactive:   titleBar,
				DarkModeTitleText:          titleText,
				DarkModeTitleTextInactive:  titleTextDim,
				DarkModeBorder:             titleBar,
				DarkModeBorderInactive:     titleBar,
				LightModeTitleBar:          lightTitleBar,
				LightModeTitleBarInactive:  lightTitleBar,
				LightModeTitleText:         lightTitleText,
				LightModeTitleTextInactive: lightTitleTextDim,
				LightModeBorder:            lightTitleBar,
				LightModeBorderInactive:    lightTitleBar,
			},
			DisablePinchZoom: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
