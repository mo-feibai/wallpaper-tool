// wallpaper-tool 命令行入口。
//
// 模式分发:
//
//	(无参数)             托盘主程序(普通权限): Web 配置 + 定时换图
//	--once               立即换一张并退出(测试用)
//	--apply-lockscreen   读取锁屏目标文件并写注册表(经计划任务以高权限运行)
//	--install-task       注册锁屏计划任务(需要提权, 首次 UAC)
//	--remove-task        移除锁屏计划任务并清除锁屏策略(需要提权)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wallpaper-tool/internal/assets"
	"wallpaper-tool/internal/autostart"
	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/elevate"
	"wallpaper-tool/internal/logging"
	"wallpaper-tool/internal/notify"
	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/scheduler"
	"wallpaper-tool/internal/singleinstance"
	"wallpaper-tool/internal/source"
	"wallpaper-tool/internal/state"
	"wallpaper-tool/internal/tray"
	"wallpaper-tool/internal/unsplash"
	"wallpaper-tool/internal/update"
	"wallpaper-tool/internal/version"
	"wallpaper-tool/internal/wallhaven"
	"wallpaper-tool/internal/wallpaper"
	"wallpaper-tool/internal/web"
)

func main() {
	applyLock := flag.Bool("apply-lockscreen", false, "apply lock screen image from target file (run elevated via scheduled task)")
	installTask := flag.Bool("install-task", false, "register the lock screen scheduled task (requires elevation)")
	removeTask := flag.Bool("remove-task", false, "remove the scheduled task and clear lock screen policy (requires elevation)")
	once := flag.Bool("once", false, "apply next wallpaper once and exit")
	flag.Parse()

	var err error
	switch {
	case *applyLock:
		err = runApplyLockScreen()
	case *installTask:
		err = runInstallTask()
	case *removeTask:
		err = runRemoveTask()
	case *once:
		err = runOnce()
	default:
		runTray()
		return
	}
	if err != nil {
		os.Exit(1)
	}
}

func loadCfgAndState() (config.Config, *state.Store, error) {
	if err := paths.EnsureDirs(); err != nil {
		return config.Config{}, nil, err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v (use defaults)\n", err)
	}
	st, err := state.Load(paths.StateFile())
	if err != nil {
		fmt.Fprintf(os.Stderr, "load state: %v (use empty state)\n", err)
	}
	return cfg, st, nil
}

// runTray 托盘主程序模式。
func runTray() {
	if err := paths.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "ensure dirs: %v\n", err)
		os.Exit(1)
	}
	lg := logging.Setup(paths.LogFile())
	defer logging.Recover(lg)()
	lg.Printf("=== starting tray mode ===")

	// 单实例检测: 已有实例时弹框提示并退出, 避免托盘图标重复与状态文件写竞争
	release, err := singleinstance.Acquire()
	if err != nil {
		lg.Printf("single instance check: %v", err)
		singleinstance.NotifyAlreadyRunning()
		return
	}
	defer release()

	// 注册通知身份(显示名"壁纸工具"+PNG 图标), 使系统通知带应用名称与图标
	if pngPath, err := assets.EnsurePngFile(filepath.Join(paths.BaseDir(), "icon.png")); err != nil {
		lg.Printf("write icon file: %v", err)
	} else if err := notify.EnsureIdentity("壁纸工具", pngPath); err != nil {
		lg.Printf("register notification identity: %v", err)
	}

	cfg, st, err := loadCfgAndState()
	if err != nil {
		lg.Printf("init: %v", err)
		os.Exit(1)
	}

	sources := buildSources(cfg, lg)
	sched := scheduler.New(cfg, st, sources, lg)

	// 启动时检查缓存目录是否被(手工改配置)变更, 需要则迁移
	if err := sched.SyncCacheDir(); err != nil {
		lg.Printf("cache dir sync: %v", err)
	}

	// 按配置同步开机自启
	exe := paths.ExePath()
	if cfg.Autostart {
		if err := autostart.Enable(exe); err != nil {
			lg.Printf("autostart enable: %v", err)
		}
	} else {
		if err := autostart.Disable(); err != nil {
			lg.Printf("autostart disable: %v", err)
		}
	}

	srv := web.New(sched, st, lg)
	port, err := srv.Start()
	if err != nil {
		lg.Printf("web server start: %v", err)
	} else {
		lg.Printf("web config page: http://127.0.0.1:%d", port)
		_ = st.Update(func(u *state.State) { u.WebPort = port })
	}

	sched.Start()
	defer sched.Stop()

	// 后台检查 GitHub 是否有新版本(失败静默, 绝不打断换图)。
	go func() {
		defer logging.Recover(lg)()
		checkUpdates(lg)
	}()

	tray.Run(tray.Deps{
		Log:         lg,
		Next:        sched.Next,
		Prev:        sched.Prev,
		TogglePause: sched.TogglePause,
		Paused:      func() bool { return sched.Status().Paused },
		ConfigURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		FolderPath:  paths.ResolveCacheDir(cfg.Cache.Dir),
		Stop:        srv.Stop,
	})
	lg.Printf("=== tray mode exited ===")
}

// checkUpdates 定期检查 GitHub 上的最新 tag, 并把结果存入 update 包供 Web 接口读取。
// 首次检查在后台 goroutine 中进行, 不阻塞启动; 之后每 6 小时查一次。
func checkUpdates(lg *log.Logger) {
	check := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		info := update.Check(ctx, version.Current())
		update.SetInfo(info)
		lg.Printf("update check: %s", info.Describe())
	}
	check()
	for range time.Tick(6 * time.Hour) {
		check()
	}
}

// buildSources 创建已注册的数据源集合(按名字索引)。
// 各源共享同一份配置与代理设置, 调度器按目标选择使用哪个源。
func buildSources(cfg config.Config, lg *log.Logger) map[string]source.Source {
	wh := wallhaven.NewClient(lg)
	wh.SetProxy(cfg.Proxy)
	wh.Update(cfg)

	us := unsplash.NewClient(lg)
	us.SetProxy(cfg.Proxy)
	us.Update(cfg)

	return map[string]source.Source{
		config.SourceWallhaven: wh,
		config.SourceUnsplash:  us,
	}
}

// runOnce 单次换图模式(测试用)。
func runOnce() error {
	lg := logging.Setup(paths.LogFile())
	cfg, st, err := loadCfgAndState()
	if err != nil {
		lg.Printf("init: %v", err)
		return err
	}
	sources := buildSources(cfg, lg)
	sched := scheduler.New(cfg, st, sources, lg)
	if err := sched.Next(); err != nil {
		lg.Printf("once failed: %v", err)
		return err
	}
	lg.Printf("once done")
	return nil
}

// readLockScreenTarget 读取锁屏目标文件, 自动去除 UTF-8 BOM 与首尾空白。
func readLockScreenTarget() (string, error) {
	data, err := os.ReadFile(paths.LockScreenTargetFile())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimPrefix(string(data), "\xEF\xBB\xBF")), nil
}

// runApplyLockScreen 助手模式: 读取目标文件并写注册表。
// 由锁屏计划任务以最高权限运行。
func runApplyLockScreen() error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	lg := logging.Setup(paths.LogFile())
	p, err := readLockScreenTarget()
	if err != nil {
		lg.Printf("read lock screen target: %v", err)
		return err
	}
	if p == "" {
		lg.Printf("lock screen target is empty")
		return fmt.Errorf("lock screen target is empty")
	}
	if err := wallpaper.ApplyLockScreen(p); err != nil {
		lg.Printf("apply lock screen failed: %v", err)
		return err
	}
	lg.Printf("lock screen applied: %s", p)
	return nil
}

// runInstallTask 注册锁屏计划任务(运行于 runas 提权进程内)。
// 注册成功后若已有目标图片则立即应用一次。
func runInstallTask() error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	lg := logging.Setup(paths.LogFile())
	exe := paths.ExePath()
	if err := elevate.InstallTask(exe); err != nil {
		lg.Printf("install task failed: %v", err)
		return err
	}
	if p, err := readLockScreenTarget(); err == nil && p != "" {
		if _, err := os.Stat(p); err == nil {
			if err := wallpaper.ApplyLockScreen(p); err != nil {
				lg.Printf("apply lock screen after install failed: %v", err)
			} else {
				lg.Printf("lock screen applied after install: %s", p)
			}
		}
	}
	lg.Printf("scheduled task installed: %s", elevate.TaskName)
	// 同步安装标记, 使主程序无需轮询即可感知
	if st, err := state.Load(paths.StateFile()); err == nil {
		_ = st.Update(func(u *state.State) { u.TaskInstalled = true })
	}
	return nil
}

// runRemoveTask 移除计划任务并清除锁屏策略(运行于 runas 提权进程内)。
func runRemoveTask() error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	lg := logging.Setup(paths.LogFile())
	if err := elevate.RemoveTask(); err != nil {
		lg.Printf("remove task failed: %v", err)
		return err
	}
	if err := wallpaper.ClearLockScreen(); err != nil {
		lg.Printf("clear lock screen failed: %v", err)
		return err
	}
	lg.Printf("scheduled task removed and lock screen policy cleared")
	// 同步清除安装标记, 避免残留 true 导致后续触发失败
	if st, err := state.Load(paths.StateFile()); err == nil {
		_ = st.Update(func(u *state.State) {
			u.TaskInstalled = false
			u.LockScreenTarget = ""
		})
	}
	return nil
}
