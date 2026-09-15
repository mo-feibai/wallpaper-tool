// Package tray 实现系统托盘菜单。
package tray

import (
	"log"
	"os/exec"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"wallpaper-tool/internal/assets"
)

var iconData = assets.Icon()

var (
	u32              = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW  = u32.NewProc("MessageBoxW")
)

// Deps 托盘依赖的外部能力, 由 main 注入。
type Deps struct {
	Log         *log.Logger
	Next        func() error
	Prev        func() error
	TogglePause func() error
	Paused      func() bool
	ConfigURL   string
	FolderPath  string
	Stop        func()
}

// Run 阻塞运行托盘, 退出后调用 Deps.Stop。
func Run(d Deps) {
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTooltip("壁纸工具 WallpaperTool")
		mNext := systray.AddMenuItem("下一张", "立即更换壁纸")
		mPrev := systray.AddMenuItem("上一张", "回退到上一张")
		mPause := systray.AddMenuItem("暂停", "暂停或继续自动更换")
		systray.AddSeparator()
		mConfig := systray.AddMenuItem("打开配置", "在浏览器中打开 Web 配置页")
		mFolder := systray.AddMenuItem("打开壁纸文件夹", "打开本地壁纸缓存目录")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出程序")

		go func() {
			for {
				select {
				case <-mNext.ClickedCh:
					if err := d.Next(); err != nil {
						d.Log.Printf("tray next: %v", err)
					}
				case <-mPrev.ClickedCh:
					if err := d.Prev(); err != nil {
						d.Log.Printf("tray prev: %v", err)
					}
				case <-mPause.ClickedCh:
					if err := d.TogglePause(); err != nil {
						d.Log.Printf("tray pause: %v", err)
					}
				case <-mConfig.ClickedCh:
					openURL(d.ConfigURL)
				case <-mFolder.ClickedCh:
					_ = exec.Command("explorer.exe", d.FolderPath).Start()
			case <-mQuit.ClickedCh:
				if confirmQuit() {
					systray.Quit()
				}
				}
			}
		}()

		// 同步暂停/继续菜单文字(定时轮询, 与 Web 端操作保持一致)
		go func() {
			last := ""
			for {
				cur := "暂停"
				if d.Paused() {
					cur = "继续"
				}
				if cur != last {
					mPause.SetTitle(cur)
					last = cur
				}
				time.Sleep(1 * time.Second)
			}
		}()
	}, func() {
		if d.Stop != nil {
			d.Stop()
		}
	})
}

func openURL(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// confirmQuit 退出前弹确认框, 防止托盘菜单误点导致程序退出、配置页断连。
// 返回用户是否确认(MB_OKCANCEL, IDOK=1)。
func confirmQuit() bool {
	const mbOkCancelIconQuestion = 0x21
	title, _ := windows.UTF16PtrFromString("壁纸工具")
	text, _ := windows.UTF16PtrFromString("确定退出壁纸工具吗? 退出后自动换图与配置页将停止服务。")
	r, _, _ := procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbOkCancelIconQuestion)
	return r == 1
}
