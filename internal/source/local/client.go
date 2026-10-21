// Package local 实现"本地目录"壁纸数据源。
//
// 用户把已有的图片文件夹交给程序, 程序按筛选条件从中取图,
// 不需要任何 API Key, 也不依赖网络。
package local

import (
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/source"
	"wallpaper-tool/internal/webimage"
)

// SourceName 数据源名, 与配置里的 sources.<target> 取值一致。
const SourceName = "local"

// pageSize 每页取多少张(与远端源的批量大小一致)。
const pageSize = 30

// 支持的图片扩展名。
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true,
}

// Client 扫描本地目录里的图片。
type Client struct {
	dir string
	lg  *log.Logger
}

// New 创建本地目录数据源。
func New(lg *log.Logger) *Client {
	return &Client{lg: lg}
}

// Name 返回数据源名。
func (c *Client) Name() string { return SourceName }

// Update 应用最新配置(目录可能变了)。
func (c *Client) Update(cfg config.Config) {
	c.dir = cfg.Local.Dir
}

// Dir 返回当前配置的目录。
func (c *Client) Dir() string { return c.dir }

// Search 扫描目录, 按条件筛选后分页返回。
func (c *Client) Search(q source.Query) (source.Result, error) {
	dir := c.dir
	if dir == "" {
		return source.Result{}, fmt.Errorf("未配置本地壁纸目录")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return source.Result{}, fmt.Errorf("读取本地目录失败: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return source.Result{}, fmt.Errorf("本地目录里没有可用的图片: %s", dir)
	}
	sort.Strings(names)

	// 用 seed 决定本轮顺序, 每轮换一个 seed 就等于重新洗牌。
	seed := q.Seed
	if seed == "" {
		seed = fmt.Sprintf("%d", len(names))
	}
	rnd := rand.New(rand.NewSource(int64(hashString(seed))))
	rnd.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })

	items := make([]source.Item, 0, len(names))
	for _, name := range names {
		full := filepath.Join(dir, name)
		w, h, err := webimage.Dimensions(full)
		if err != nil {
			// 读不出尺寸的文件跳过(损坏或不支持的格式)
			continue
		}
		if q.MinWidth > 0 && w < q.MinWidth {
			continue
		}
		if q.MinHeight > 0 && h < q.MinHeight {
			continue
		}
		if !matchOrientation(q.Orientation, w, h) {
			continue
		}
		items = append(items, source.Item{
			ID:       name,
			Source:   SourceName,
			FileURL:  full,
			PageURL:  "",
			FileType: mimeOf(name),
			Title:    name,
			Width:    w,
			Height:   h,
		})
	}
	if len(items) == 0 {
		return source.Result{}, fmt.Errorf("本地目录里没有符合筛选条件的图片: %s", dir)
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	lastPage := (len(items) + pageSize - 1) / pageSize
	start := (page - 1) * pageSize
	if start >= len(items) {
		start = 0
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return source.Result{
		Items:    items[start:end],
		LastPage: lastPage,
		Seed:     fmt.Sprintf("%d-%d", len(items), hashString(nameOf(items))),
	}, nil
}

// Download 把本地图片复制到缓存目录, 命名规则与其它源一致({source}-{id}).
func (c *Client) Download(item source.Item, destDir string) (string, error) {
	src := item.FileURL
	if src == "" {
		return "", fmt.Errorf("本地条目缺少路径: %s", item.ID)
	}
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("本地文件不存在: %s", src)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, SourceName+"-"+item.ID)
	// 已存在且大小一致时直接复用, 避免重复复制
	if st, err := os.Stat(dest); err == nil {
		if sst, serr := os.Stat(src); serr == nil && st.Size() == sst.Size() {
			return dest, nil
		}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	tmp := dest + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if c.lg != nil {
		c.lg.Printf("local image copied: %s -> %s", src, dest)
	}
	return dest, nil
}

// matchOrientation 按方向过滤。
func matchOrientation(orientation string, w, h int) bool {
	switch orientation {
	case "landscape":
		return w > h
	case "portrait":
		return h > w
	case "squarish":
		d := w - h
		return d <= 0 && h-w <= 0
	default:
		return true
	}
}

// mimeOf 由扩展名推断 MIME。
func mimeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}

// nameOf 取条目名用于生成稳定 seed。
func nameOf(items []source.Item) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].ID
}

// hashString 返回字符串的稳定哈希值。
func hashString(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
