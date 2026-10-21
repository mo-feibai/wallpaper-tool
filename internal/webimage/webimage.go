// Package webimage 生成配置页展示用的派生图片(缩略图/预览/背景)。
//
// 背景: 配置页原先直接加载缓存目录里的原图(宽度可达 3840px),
// 历史区 22 张缩略图合计约 24MB / 1.38 亿像素,
// 浏览器解码开销成为滚动卡顿的主因(实测单次解码最长 232ms)。
//
// 本包把原图等比缩放到适合网页展示的尺寸并缓存到独立目录:
// 派生图只用于网页展示, 桌面与锁屏实际使用的仍然是原图。
// 派生缓存独立于壁纸缓存目录, 不计入壁纸张数/天数限制, 也不参与目录迁移。
package webimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"

	"wallpaper-tool/internal/state"
)

const (
	// algoVersion 缩放与编码策略版本, 改动策略时递增, 旧派生图自动失效。
	// v2: 修正 Overlay 返回值被丢弃导致派生图只剩底色的问题。
	algoVersion = "v2"
	// maxSourceBytes 单张原图体积上限, 超过则不生成派生图。
	maxSourceBytes = 64 << 20
	// maxSourcePixels 原图像素上限, 超过则不生成派生图。
	maxSourcePixels = 40000000
	// cacheBudget 派生缓存目录容量上限。
	cacheBudget = 128 << 20
	// maxQueue 后台预生成任务队列上限。
	maxQueue = 32
	// decodeSlots 同时进行的解码数, 限制 CPU 占用。
	decodeSlots = 2
	// prewarmTimeout 后台预生成等待解码许可的最长时间, 超时则跳过本次预热。
	prewarmTimeout = 200 * time.Millisecond
)

// Variant 派生图片的用途。
type Variant string

const (
	// VariantThumb 历史列表缩略图。
	VariantThumb Variant = "thumb"
	// VariantPreview 当前壁纸预览图。
	VariantPreview Variant = "preview"
	// VariantBackground 页面背景图。
	VariantBackground Variant = "background"
)

// State 派生图片的准备状态。
type State string

const (
	// StateReady 派生图已生成, 可直接显示。
	StateReady State = "ready"
	// StatePending 原图存在但派生图尚未生成, 首次请求时按需生成。
	StatePending State = "pending"
	// StateUnavailable 原图不存在或不可用。
	StateUnavailable State = "unavailable"
)

// spec 一种用途对应的输出规格。
type spec struct {
	maxW    int
	maxH    int
	quality int
}

// specs 各用途的输出规格(等比缩放, 不放大原图)。
var specs = map[Variant]spec{
	VariantThumb:      {maxW: 320, maxH: 200, quality: 78},
	VariantPreview:    {maxW: 1280, maxH: 800, quality: 82},
	VariantBackground: {maxW: 1920, maxH: 1200, quality: 72},
}

// priority 返回后台任务的优先级, 预览与背景高于历史缩略图。
func (v Variant) priority() int {
	switch v {
	case VariantPreview, VariantBackground:
		return 10
	default:
		return 1
	}
}

// Info 描述一张壁纸在网页展示上的信息。
type Info struct {
	Version       string `json:"version"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	State         State  `json:"state"`
	ThumbURL      string `json:"thumb_url"`
	PreviewURL    string `json:"preview_url"`
	BackgroundURL string `json:"background_url"`
}

// dim 记录一张原图的方向校正后尺寸。
type dim struct{ w, h int }

// task 一次派生图生成任务。
type task struct {
	key      string
	priority int
	run      func() (string, error)
	done     chan struct{}
	result   string
	err      error
}

// Manager 管理派生图片的生成与缓存。
type Manager struct {
	dir string
	log *log.Logger
	sem chan struct{}

	mu     sync.Mutex
	queue  []*task
	active map[string]*task
	access map[string]time.Time
	dims   map[string]dim
	closed bool

	wake chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup
}

// New 创建管理器。dir 为派生缓存目录, 由调用方决定位置。
func New(dir string, lg *log.Logger) *Manager {
	if lg == nil {
		lg = log.New(os.Stderr, "", 0)
	}
	return &Manager{
		dir:    dir,
		log:    lg,
		sem:    make(chan struct{}, decodeSlots),
		active: map[string]*task{},
		access: map[string]time.Time{},
		dims:   map[string]dim{},
		wake:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
	}
}

// Start 启动后台预生成协程。
func (m *Manager) Start() { m.wg.Add(1); go m.worker() }

// Close 停止后台协程。
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	close(m.stop)
	m.wg.Wait()
}

// worker 后台预生成: 逐个取出队列任务并生成派生图。
func (m *Manager) worker() {
	defer m.wg.Done()
	defer m.recoverPanic()
	for {
		t := m.nextTask()
		if t == nil {
			select {
			case <-m.wake:
			case <-m.stop:
				return
			}
			continue
		}
		m.runTask(t, true)
	}
}

// recoverPanic 防止后台协程的 panic 让整个进程退出(程序无控制台, 静默退出无法排查)。
func (m *Manager) recoverPanic() {
	if r := recover(); r != nil {
		m.log.Printf("web image worker panic: %v\n%s", r, debug.Stack())
	}
}

// nextTask 取出优先级最高的任务。
func (m *Manager) nextTask() *task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return nil
	}
	best := 0
	for i, t := range m.queue {
		if t.priority > m.queue[best].priority {
			best = i
		}
	}
	t := m.queue[best]
	m.queue = append(m.queue[:best], m.queue[best+1:]...)
	return t
}

// runTask 执行任务并广播结果。allowSkip 为真时拿不到解码许可就直接跳过。
func (m *Manager) runTask(t *task, allowSkip bool) {
	p, err := m.runLimited(t.run, allowSkip)
	t.result, t.err = p, err
	close(t.done)
	m.mu.Lock()
	delete(m.active, t.key)
	m.mu.Unlock()
	if err != nil && !errors.Is(err, errPrewarmSkipped) {
		m.log.Printf("web image generate failed: %v", err)
	}
}

// errPrewarmSkipped 表示预热任务因资源繁忙被跳过, 不算错误。
var errPrewarmSkipped = errors.New("webimage: 预热跳过")

// runLimited 在解码并发限制内执行任务。
func (m *Manager) runLimited(fn func() (string, error), allowSkip bool) (string, error) {
	if allowSkip {
		select {
		case m.sem <- struct{}{}:
		case <-time.After(prewarmTimeout):
			return "", errPrewarmSkipped
		}
	} else {
		m.sem <- struct{}{}
	}
	defer func() { <-m.sem }()
	return fn()
}

// Ensure 确保派生图已生成并返回其路径; 已存在则直接返回。
func (m *Manager) Ensure(it state.Item, v Variant) (string, error) {
	key, out, err := m.derivedPath(it, v)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(out); err == nil {
		m.touch(out)
		return out, nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", errors.New("webimage: 管理器已关闭")
	}
	if t, ok := m.active[key]; ok {
		m.mu.Unlock()
		<-t.done
		if t.err != nil {
			return "", t.err
		}
		return t.result, nil
	}
	t := &task{key: key, priority: v.priority(), done: make(chan struct{}), run: func() (string, error) {
		return m.generate(it, v)
	}}
	m.active[key] = t
	m.mu.Unlock()

	m.runTask(t, false)
	if t.err != nil {
		return "", t.err
	}
	return t.result, nil
}

// Prewarm 把派生图生成排入后台队列, 不阻塞调用方。
func (m *Manager) Prewarm(it state.Item, v Variant) {
	key, out, err := m.derivedPath(it, v)
	if err != nil {
		return
	}
	if _, err := os.Stat(out); err == nil {
		m.touch(out)
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if _, ok := m.active[key]; ok {
		m.mu.Unlock()
		return
	}
	if len(m.queue) >= maxQueue {
		m.mu.Unlock()
		return
	}
	t := &task{key: key, priority: v.priority(), done: make(chan struct{}), run: func() (string, error) {
		return m.generate(it, v)
	}}
	m.active[key] = t
	m.queue = append(m.queue, t)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// derivedPath 计算派生图的目标路径与任务键。
func (m *Manager) derivedPath(it state.Item, v Variant) (string, string, error) {
	if _, ok := specs[v]; !ok {
		return "", "", fmt.Errorf("webimage: 未知用途 %q", v)
	}
	if it.Path == "" {
		return "", "", errors.New("webimage: 壁纸没有本地文件")
	}
	ver, err := m.version(it)
	if err != nil {
		return "", "", err
	}
	name := fmt.Sprintf("%s-%s-%s-%s.jpg", sanitize(it.Source), sanitize(it.ID), string(v), ver)
	return string(v) + ":" + ver, filepath.Join(m.dir, name), nil
}

// version 依据来源、ID、文件路径与时间戳生成稳定版本号。
// 原图被替换或缓存目录迁移后版本随之变化, 旧的派生图不会被误用。
func (m *Manager) version(it state.Item) (string, error) {
	info, err := os.Stat(it.Path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d|%d|%s",
		it.Source, it.ID, it.Path, info.Size(), info.ModTime().UnixNano(), algoVersion)
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// generate 解码原图、缩放并写出派生图。
func (m *Manager) generate(it state.Item, v Variant) (string, error) {
	sp := specs[v]
	info, err := os.Stat(it.Path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxSourceBytes {
		return "", fmt.Errorf("webimage: 原图过大(%d 字节)", info.Size())
	}
	src, err := imaging.Open(it.Path, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}
	b := src.Bounds()
	if b.Dx()*b.Dy() > maxSourcePixels {
		return "", fmt.Errorf("webimage: 原图像素过大(%dx%d)", b.Dx(), b.Dy())
	}
	dst := imaging.Fit(src, sp.maxW, sp.maxH, imaging.Lanczos)
	// 透明区域铺深色底, 避免 JPEG 编码后出现纯黑块。
	// 注意: imaging.Overlay 返回新图, 必须接收返回值, 否则底色会覆盖内容。
	flat := imaging.New(dst.Bounds().Dx(), dst.Bounds().Dy(), color.NRGBA{R: 18, G: 20, B: 28, A: 255})
	flat = imaging.Overlay(flat, dst, image.Pt(0, 0), 1.0)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: sp.quality}); err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", err
	}
	_, out, err := m.derivedPath(it, v)
	if err != nil {
		return "", err
	}
	tmp := out + "." + strconv.FormatInt(time.Now().UnixNano(), 10) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	m.touch(out)
	return out, nil
}

// VersionOf 返回壁纸的派生图版本号, 用于在重复 ID 时区分不同文件。
func (m *Manager) VersionOf(it state.Item) string {
	v, err := m.version(it)
	if err != nil {
		return ""
	}
	return v
}

// Describe 返回一张壁纸的网页展示信息。
// kind 为 "desktop" 或 "lock", 决定图片地址中的查询参数。
// primary 为该条目的主要用途, 决定 ready/pending 判定。
func (m *Manager) Describe(kind string, it state.Item, primary Variant) Info {
	info := Info{State: StateUnavailable}
	if it.Path == "" {
		return info
	}
	ver, err := m.version(it)
	if err != nil {
		return info
	}
	info.Version = ver
	w, h, err := m.dimensions(it, ver)
	if err != nil {
		return info
	}
	info.Width, info.Height = w, h
	info.ThumbURL = m.url(kind, it, VariantThumb, ver)
	info.PreviewURL = m.url(kind, it, VariantPreview, ver)
	info.BackgroundURL = m.url(kind, it, VariantBackground, ver)

	_, out, err := m.derivedPath(it, primary)
	if err != nil {
		return info
	}
	if _, err := os.Stat(out); err == nil {
		info.State = StateReady
		m.touch(out)
	} else {
		info.State = StatePending
	}
	return info
}

// url 构造派生图地址。地址中带版本号, 浏览器可长期缓存。
func (m *Manager) url(kind string, it state.Item, v Variant, ver string) string {
	src := it.Source
	if src == "" {
		src = "wallhaven"
	}
	q := url.Values{}
	q.Set("type", kind)
	q.Set("source", src)
	q.Set("id", it.ID)
	q.Set("variant", string(v))
	q.Set("v", ver)
	return "/api/display-image?" + q.Encode()
}

// dimensions 返回原图方向校正后的尺寸, 结果按版本缓存。
func (m *Manager) dimensions(it state.Item, ver string) (int, int, error) {
	m.mu.Lock()
	if d, ok := m.dims[ver]; ok {
		m.mu.Unlock()
		return d.w, d.h, nil
	}
	m.mu.Unlock()
	w, h, err := imageDimensions(it.Path)
	if err != nil {
		return 0, 0, err
	}
	m.mu.Lock()
	m.dims[ver] = dim{w, h}
	m.mu.Unlock()
	return w, h, nil
}

// touch 记录派生图的最近访问时间, 供容量淘汰使用。
func (m *Manager) touch(path string) {
	name := filepath.Base(path)
	m.mu.Lock()
	m.access[name] = time.Now()
	m.mu.Unlock()
}

// Cleanup 在容量超限时按最近使用时间淘汰派生图。
func (m *Manager) Cleanup() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		used time.Time
	}
	files := make([]file, 0, len(entries))
	total := int64(0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		used := info.ModTime()
		m.mu.Lock()
		if t, ok := m.access[e.Name()]; ok {
			used = t
		}
		m.mu.Unlock()
		files = append(files, file{filepath.Join(m.dir, e.Name()), info.Size(), used})
	}
	if total <= cacheBudget {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	target := int64(cacheBudget) * 9 / 10
	for _, f := range files {
		if total <= target {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
	m.log.Printf("web image cache trimmed: total=%d bytes", total)
}

// sanitize 过滤出可安全用于文件名的字符。
func sanitize(s string) string {
	if s == "" {
		return "wallhaven"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" {
		return "wallhaven"
	}
	return out
}
