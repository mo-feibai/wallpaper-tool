// Package config 负责配置的定义、默认值、校验和 YAML 读写。
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"wallpaper-tool/internal/paths"
)

// Categories wallhaven 分类开关。
type Categories struct {
	General bool `yaml:"general" json:"general"`
	Anime   bool `yaml:"anime" json:"anime"`
	People  bool `yaml:"people" json:"people"`
}

// Purity 内容分级开关。匿名接口不支持 NSFW，此处永不开启。
type Purity struct {
	SFW     bool `yaml:"sfw" json:"sfw"`
	Sketchy bool `yaml:"sketchy" json:"sketchy"`
}

// Targets 应用目标：桌面与锁屏。
type Targets struct {
	Desktop    bool `yaml:"desktop" json:"desktop"`
	LockScreen bool `yaml:"lock_screen" json:"lock_screen"`
}

// Cache 缓存设置。
// Dir 为空时使用默认目录 %APPDATA%\wallpaper-tool\cache;
// 支持 Windows 环境变量写法(如 %USERPROFILE%\Pictures\wallpapers)。
// MaxDays 为 0 表示不按天数清理。
type Cache struct {
	Dir      string `yaml:"dir" json:"dir"`
	MaxItems int    `yaml:"max_items" json:"max_items"`
	MaxDays  int    `yaml:"max_days" json:"max_days"`
}

// 可用数据源名。
const (
	SourceWallhaven = "wallhaven"
	SourceUnsplash  = "unsplash"
	// SourceLocal 本地目录数据源: 从用户自己的图片文件夹取图。
	SourceLocal = "local"
)

// Local 本地目录数据源设置。
// Dir 为空时该数据源不可用。
type Local struct {
	Dir string `yaml:"dir" json:"dir"`
}

// Theme 主题联动设置。
// AccentFromWallpaper: 按壁纸主色设置 Windows 强调色。
// AutoLightDark: 按壁纸明暗自动切换系统深/浅色模式。
type Theme struct {
	AccentFromWallpaper bool `yaml:"accent_from_wallpaper" json:"accent_from_wallpaper"`
	AutoLightDark       bool `yaml:"auto_light_dark" json:"auto_light_dark"`
}

// SourceConfig 指定各目标使用的数据源。
// 桌面与锁屏可以分别选择不同的数据源。
type SourceConfig struct {
	Desktop    string `yaml:"desktop" json:"desktop"`
	LockScreen string `yaml:"lock_screen" json:"lock_screen"`
}

// Web Web 配置服务设置。
type Web struct {
	Port int `yaml:"port" json:"port"` // 0 表示随机空闲端口
}

// Config 顶层配置结构。
// Revision 配置版本号, 每次成功保存自增。
// Web 保存时校验客户端持有的版本, 防止长时间未刷新的旧页面覆盖较新配置。
type Config struct {
	Revision int `yaml:"revision" json:"revision"`

	// Proxy 访问数据源的代理地址, 例 http://127.0.0.1:20112。
	// 留空自动使用 Windows 系统代理(浏览器同款), 再退回环境变量, 最后直连。
	Proxy string `yaml:"proxy" json:"proxy"`

	// Sources 各目标使用的数据源。
	Sources SourceConfig `yaml:"sources" json:"sources"`

	// Local 本地目录数据源的图片目录。
	Local Local `yaml:"local" json:"local"`

	// Theme 主题联动(取色)。
	Theme Theme `yaml:"theme" json:"theme"`

	// UnsplashKey Unsplash Access Key, 仅在选择 unsplash 数据源时需要。
	// 属于凭据, 不回显到 Web 接口, 也不写进日志。
	UnsplashKey string `yaml:"unsplash_key" json:"unsplash_key"`

	IntervalMinutes int        `yaml:"interval_minutes" json:"interval_minutes"`
	Categories      Categories `yaml:"categories" json:"categories"`
	Purity          Purity     `yaml:"purity" json:"purity"`
	Query           string     `yaml:"query" json:"query"`
	Sorting         string     `yaml:"sorting" json:"sorting"`
	Order           string     `yaml:"order" json:"order"`
	TopRange        string     `yaml:"top_range" json:"top_range"`
	AtLeast         string     `yaml:"at_least" json:"at_least"`
	Ratios          string     `yaml:"ratios" json:"ratios"`
	Orientation     string     `yaml:"orientation" json:"orientation"` // "" | landscape | portrait, 非空时覆盖 ratios
	Targets         Targets    `yaml:"targets" json:"targets"`
	DesktopFit      string     `yaml:"desktop_fit" json:"desktop_fit"`
	// PauseWhenFullscreen 全屏时跳过自动换图(手动换图不受影响)。
	// 用指针区分"未设置"与"显式关闭": 老版本配置里没有这个键,
	// 应当沿用默认开启, 而不是被当成关闭。
	PauseWhenFullscreen *bool `yaml:"pause_when_fullscreen" json:"pause_when_fullscreen"`
	Autostart           bool  `yaml:"autostart" json:"autostart"`
	Cache               Cache `yaml:"cache" json:"cache"`
	Web                 Web   `yaml:"web" json:"web"`
}

// Default 返回默认配置。
func Default() Config {
	return Config{
		IntervalMinutes:     30,
		Categories:          Categories{General: true, Anime: true, People: false},
		Purity:              Purity{SFW: true, Sketchy: false},
		Sorting:             "random",
		Order:               "desc",
		TopRange:            "1M",
		AtLeast:             "1920x1080",
		Sources:             SourceConfig{Desktop: SourceWallhaven, LockScreen: SourceWallhaven},
		Targets:             Targets{Desktop: true, LockScreen: false},
		DesktopFit:          "fill",
		PauseWhenFullscreen: boolPtr(true),
		Cache:               Cache{MaxItems: 20},
		Web:                 Web{Port: 0},
	}
}

var validSorting = map[string]bool{
	"random": true, "toplist": true, "date_added": true,
	"views": true, "favorites": true, "relevance": true,
}

var validTopRange = map[string]bool{
	"1d": true, "3d": true, "1w": true, "1M": true, "3M": true, "6M": true, "1y": true,
}

var validFit = map[string]bool{
	"fill": true, "fit": true, "stretch": true, "center": true, "tile": true, "span": true,
}

// validSource 判断数据源名是否受支持。
func validSource(name string) bool {
	return name == SourceWallhaven || name == SourceUnsplash || name == SourceLocal
}

var resRe = regexp.MustCompile(`^[0-9]{2,5}x[0-9]{2,5}$`)
var ratioRe = regexp.MustCompile(`^[0-9]{1,3}x[0-9]{1,3}$`)

// boolPtr 返回指向 v 的指针, 便于给 *bool 字段赋默认值。
func boolPtr(v bool) *bool { return &v }

// Sanitize 就地修正非法配置值，保证写回磁盘与发送给 API 的值始终合法。
func (c *Config) Sanitize() {
	if c.PauseWhenFullscreen == nil {
		// 老版本配置里没有这个键 -> 沿用默认开启
		c.PauseWhenFullscreen = boolPtr(true)
	}
	if c.IntervalMinutes < 1 {
		c.IntervalMinutes = 1
	}
	if c.IntervalMinutes > 10080 { // 上限一周
		c.IntervalMinutes = 10080
	}
	if !c.Categories.General && !c.Categories.Anime && !c.Categories.People {
		c.Categories.General = true
	}
	if !c.Purity.SFW && !c.Purity.Sketchy {
		c.Purity.SFW = true
	}
	if !validSorting[c.Sorting] {
		c.Sorting = "random"
	}
	if c.Order != "asc" {
		c.Order = "desc"
	}
	if !validTopRange[c.TopRange] {
		c.TopRange = "1M"
	}
	c.Query = strings.TrimSpace(c.Query)
	c.Proxy = strings.TrimSpace(c.Proxy)
	c.UnsplashKey = strings.TrimSpace(c.UnsplashKey)
	if !validSource(c.Sources.Desktop) {
		c.Sources.Desktop = SourceWallhaven
	}
	if !validSource(c.Sources.LockScreen) {
		c.Sources.LockScreen = SourceWallhaven
	}
	c.Local.Dir = normalizeDir(c.Local.Dir)
	c.AtLeast = normalizeSize(c.AtLeast)
	c.Ratios = normalizeRatioList(c.Ratios)
	if c.Orientation != "landscape" && c.Orientation != "portrait" {
		c.Orientation = ""
	}
	if !validFit[c.DesktopFit] {
		c.DesktopFit = "fill"
	}
	c.Cache.Dir = normalizeDir(c.Cache.Dir)
	if c.Cache.MaxItems < 1 {
		c.Cache.MaxItems = 20
	}
	if c.Cache.MaxItems > 5000 {
		c.Cache.MaxItems = 5000
	}
	if c.Cache.MaxDays < 0 {
		c.Cache.MaxDays = 0
	}
	if c.Cache.MaxDays > 36500 {
		c.Cache.MaxDays = 36500
	}
	if c.Web.Port < 0 || c.Web.Port > 65535 {
		c.Web.Port = 0
	}
}

func normalizeSize(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	if !resRe.MatchString(s) {
		return ""
	}
	return s
}

func normalizeRatioList(s string) string {
	parts := strings.Split(s, ",")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if ratioRe.MatchString(p) {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// envVarRe 匹配 Windows 风格环境变量引用 %NAME%。
var envVarRe = regexp.MustCompile(`%([^%]+)%`)

// expandWindowsEnv 展开 %VAR% 形式的环境变量, 未定义的保持原样。
func expandWindowsEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.Trim(m, "%")
		if v := os.Getenv(name); v != "" {
			return v
		}
		return m
	})
}

// normalizeDir 规范缓存目录: 展开环境变量、相对路径基于数据根目录解析。
// 返回空串表示使用默认目录。
func normalizeDir(s string) string {
	s = expandWindowsEnv(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(paths.BaseDir(), s)
	}
	return filepath.Clean(s)
}

// Load 读取配置文件。文件不存在时写入默认配置并返回。
// 解析失败时返回默认配置与错误，由调用方决定如何记录。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg := Default()
		cfg.Sanitize()
		_ = Save(path, cfg)
		return cfg, nil
	}
	if err != nil {
		return Default(), err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	cfg.Sanitize()
	return cfg, nil
}

// Save 将配置写入 YAML 文件(先经过 Sanitize)。
func Save(path string, c Config) error {
	c.Sanitize()
	data, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
