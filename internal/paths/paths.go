// Package paths 集中管理本工具的数据目录与文件路径。
// 所有数据存放在 %APPDATA%\wallpaper-tool 下。
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// BaseDir 返回数据根目录 %APPDATA%\wallpaper-tool。
func BaseDir() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		appdata = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appdata, "wallpaper-tool")
}

// ConfigFile 返回配置文件路径。
func ConfigFile() string { return filepath.Join(BaseDir(), "config.yaml") }

// StateFile 返回状态文件路径。
func StateFile() string { return filepath.Join(BaseDir(), "state.json") }

// LogFile 返回日志文件路径。
func LogFile() string { return filepath.Join(BaseDir(), "log.txt") }

// CacheDir 返回默认壁纸缓存目录 %APPDATA%\wallpaper-tool\cache。
func CacheDir() string { return filepath.Join(BaseDir(), "cache") }

// WebImageDir 返回配置页派生图片目录 %APPDATA%\wallpaper-tool\web-images。
// 该目录只存放网页展示用的缩略图与预览图, 与壁纸缓存相互独立:
// 不计入壁纸张数/天数限制, 也不参与缓存目录迁移。
func WebImageDir() string { return filepath.Join(BaseDir(), "web-images") }

// ResolveCacheDir 解析实际使用的缓存目录。
// custom 为空时返回默认目录; 否则原样返回(已由 config.Sanitize 规范化)。
func ResolveCacheDir(custom string) string {
	if strings.TrimSpace(custom) == "" {
		return CacheDir()
	}
	return filepath.Clean(custom)
}

// LockScreenTargetFile 返回锁屏目标文件路径。
// 主程序把待应用的锁屏图片绝对路径写在这里，供 --apply-lockscreen 模式读取。
func LockScreenTargetFile() string { return filepath.Join(BaseDir(), "lockscreen_target.txt") }

// EnsureDirs 创建所需的目录结构。
func EnsureDirs() error {
	if err := os.MkdirAll(BaseDir(), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(CacheDir(), 0o755)
}

// ExePath 返回当前可执行文件的绝对路径。
func ExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
