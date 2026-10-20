// Package source 定义壁纸数据源的统一接口。
// 各数据源(wallhaven/unsplash)实现同一套接口, 调度器按目标选择数据源。
package source

import (
	"fmt"
	"io"
	"log"
	"os"

	"wallpaper-tool/internal/config"
)

// SaveImage 把响应体写入目标文件(先写临时文件再改名), 并校验大小。
// 供各数据源下载原图共用, 避免半成品文件留在缓存目录。
func SaveImage(dest string, body io.Reader, lg *log.Logger) (string, error) {
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if n < 10*1024 {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("文件过小(%d 字节), 疑似无效内容", n)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if lg != nil {
		lg.Printf("downloaded %s (%d KB)", dest, n/1024)
	}
	return dest, nil
}

// Item 表示一个可下载的壁纸条目。
type Item struct {
	ID       string // 源内唯一 ID
	Source   string // 数据源名(wallhaven/unsplash)
	FileURL  string // 原图直链
	PageURL  string // 详情页地址
	FileType string // MIME 类型, 决定落盘扩展名
	Title    string // 标题或描述
	Author   string // 摄影师署名
	Width    int    // 原图宽
	Height   int    // 原图高
}

// Query 是一次取图的查询条件。
// 源特有条件(如 wallhaven 的分类/分级)由实现自持, 不污染本结构。
type Query struct {
	Page        int    // 页码, 从 1 开始
	Seed        string // 随机序列种子, 空表示由服务端生成
	Keyword     string // 关键词
	Orientation string // landscape / portrait / squarish, 空表示不限
	MinWidth    int    // 最小宽度, 0 表示不限
	MinHeight   int    // 最小高度, 0 表示不限
}

// Result 是一次取图的结果。
type Result struct {
	Items    []Item
	LastPage int    // 已知总页数, 0 表示未知
	Seed     string // 随机序列种子, 空表示不支持
}

// Source 是壁纸数据源接口。
type Source interface {
	// Name 返回数据源名, 与配置里的 sources.<target> 取值一致。
	Name() string
	// Search 按条件取一批图。
	Search(q Query) (Result, error)
	// Download 下载原图到 destDir, 返回本地文件路径。
	Download(item Item, destDir string) (string, error)
	// Update 应用最新配置(源特有条件可能变化)。
	Update(cfg config.Config)
}
