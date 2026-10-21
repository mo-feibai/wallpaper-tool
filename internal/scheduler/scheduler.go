// Package scheduler 实现定时换图的编排核心:
// 预取队列、下载、桌面/锁屏应用、历史与缓存清理。
package scheduler

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/elevate"
	"wallpaper-tool/internal/fullscreen"
	"wallpaper-tool/internal/notify"
	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/source"
	"wallpaper-tool/internal/state"
	"wallpaper-tool/internal/theme"
	"wallpaper-tool/internal/wallpaper"
)

const historyLimit = 50

var errNoResults = errors.New("搜索结果为空或已全部看过, 请调整筛选条件")

// Status 暴露给 Web/托盘的状态快照。
type Status struct {
	Paused        bool      `json:"paused"`
	NextRun       time.Time `json:"next_run"`
	LastError     string    `json:"last_error"`
	TaskInstalled bool      `json:"task_installed"`
	// Fullscreen 表示当前有全屏窗口, 自动换图被跳过。
	Fullscreen  bool         `json:"fullscreen"`
	Current     state.Item   `json:"current"`
	LockCurrent state.Item   `json:"lock_current"`
	History     []state.Item `json:"history"`
	LockHistory []state.Item `json:"lock_history"`
}

// Scheduler 调度器。
type Scheduler struct {
	mu      sync.Mutex
	cfg     config.Config
	state   *state.Store
	sources map[string]source.Source // 按数据源名索引
	log     *log.Logger
	ticker  *time.Ticker
	stop    chan struct{}
	nextRun time.Time
	lastErr string
	started bool
}

// New 创建调度器。sources 为已注册的数据源集合。
func New(cfg config.Config, st *state.Store, sources map[string]source.Source, lg *log.Logger) *Scheduler {
	cfg.Sanitize()
	return &Scheduler{
		cfg:     cfg,
		state:   st,
		sources: sources,
		log:     lg,
	}
}

// Config 返回当前配置副本。
func (s *Scheduler) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// UpdateConfig 保存并应用新配置(重置定时器)。
// 搜索相关字段变化时清空预取队列, 避免继续使用旧筛选条件拉取的图片;
// 某目标的数据源变化时只清空该目标的队列。
func (s *Scheduler) UpdateConfig(cfg config.Config) error {
	cfg.Sanitize()
	oldSig := searchSignature(s.cfg)
	newSig := searchSignature(cfg)
	oldDesktopSrc := s.cfg.Sources.Desktop
	oldLockSrc := s.cfg.Sources.LockScreen
	if err := config.Save(paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	s.mu.Lock()
	s.cfg = cfg
	for _, sc := range s.sources {
		sc.Update(cfg)
	}
	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	if s.ticker != nil {
		s.ticker.Reset(interval)
	}
	s.nextRun = time.Now().Add(interval)
	s.mu.Unlock()

	clearDesktop := oldSig != newSig || oldDesktopSrc != cfg.Sources.Desktop
	clearLock := oldSig != newSig || oldLockSrc != cfg.Sources.LockScreen
	if clearDesktop || clearLock {
		_ = s.state.Update(func(u *state.State) {
			if clearDesktop {
				u.Desktop = state.Cursor{Queue: []state.Item{}, Page: 1}
			}
			if clearLock {
				u.Lock = state.Cursor{Queue: []state.Item{}, Page: 1}
			}
		})
		s.log.Printf("search filters or source changed, prefetch queue cleared")
	}
	return nil
}

// searchSignature 生成影响搜索结果的配置签名, 用于检测筛选条件变化。
func searchSignature(c config.Config) string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%s|%s|%s|%s|%s|%s|%s|%s",
		c.Categories.General, c.Categories.Anime, c.Categories.People,
		c.Purity.SFW, c.Purity.Sketchy,
		c.Query, c.Sorting, c.Order, c.TopRange, c.AtLeast, c.Ratios, c.Orientation,
		c.Local.Dir)
}

// Start 启动定时循环。
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stop = make(chan struct{})
	s.ticker = time.NewTicker(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	s.nextRun = time.Now().Add(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	stop := s.stop
	ticker := s.ticker
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				st := s.state.Get()
				if st.Paused {
					continue
				}
				// 全屏(游戏/演示/视频)时跳过自动换图, 观影不被壁纸切换打断。
				// 手动"下一张/上一张"不受此限制。
				cfg := s.Config()
				if cfg.PauseWhenFullscreen != nil && *cfg.PauseWhenFullscreen && fullscreen.IsFullscreen() {
					s.log.Printf("检测到全屏窗口, 本次自动换图已跳过")
					continue
				}
				if err := s.applyNext(); err != nil {
					s.setErr(err.Error())
					s.log.Printf("自动换图失败: %v", err)
				} else {
					// 换图成功后清掉上次错误, 避免失败提示一直挂着
					s.setErr("")
					s.log.Printf("自动换图成功")
				}
			}
		}
	}()
}

// Stop 停止定时循环。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.started = false
	if s.stop != nil {
		close(s.stop)
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// Next 手动换一张新图(忽略暂停状态)。
func (s *Scheduler) Next() error {
	if err := s.applyNext(); err != nil {
		s.setErr(err.Error())
		return err
	}
	s.setErr("")
	return nil
}

// Prev 回退到上一张(历史)。桌面与锁屏各自独立回退;
// 锁屏历史缺失或对应文件已被清理时, 仅回退桌面。
func (s *Scheduler) Prev() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state.Get()
	if len(st.History) == 0 || st.HistoryIndex+1 >= len(st.History) {
		err := errors.New("没有更早的历史记录")
		s.lastErr = err.Error()
		return err
	}
	idx := st.HistoryIndex + 1
	item := st.History[idx]
	if item.Path == "" {
		err := errors.New("历史记录缺少本地文件")
		s.lastErr = err.Error()
		return err
	}
	if _, err := os.Stat(item.Path); err != nil {
		err = fmt.Errorf("历史图片已被缓存清理, 无法回退")
		s.lastErr = err.Error()
		return err
	}
	if err := s.applyDesktop(item); err != nil {
		s.lastErr = err.Error()
		return err
	}
	_ = s.state.Update(func(u *state.State) {
		u.Current = item
		u.HistoryIndex = idx
	})

	// 锁屏独立回退
	lockIdx := st.LockHistoryIndex + 1
	if lockIdx < len(st.LockHistory) {
		lockItem := st.LockHistory[lockIdx]
		if lockItem.Path != "" {
			if _, err := os.Stat(lockItem.Path); err == nil {
				if err := s.applyLockScreenItem(lockItem); err != nil {
					s.log.Printf("lock screen prev failed: %v", err)
				} else {
					_ = s.state.Update(func(u *state.State) {
						u.LockCurrent = lockItem
						u.LockScreenTarget = lockItem.Path
						u.LockHistoryIndex = lockIdx
					})
				}
			} else {
				s.log.Printf("lock screen history file cleaned, skip revert: %s", lockItem.ID)
			}
		}
	}

	s.lastErr = ""
	notify.Show("更换成功", "已回退到上一张壁纸", s.log)
	return nil
}

// Pause 暂停自动更换。
func (s *Scheduler) Pause() error {
	return s.state.Update(func(u *state.State) { u.Paused = true })
}

// Resume 恢复自动更换(重新计时)。
func (s *Scheduler) Resume() error {
	if err := s.state.Update(func(u *state.State) { u.Paused = false }); err != nil {
		return err
	}
	s.mu.Lock()
	interval := time.Duration(s.cfg.IntervalMinutes) * time.Minute
	if s.ticker != nil {
		s.ticker.Reset(interval)
	}
	s.nextRun = time.Now().Add(interval)
	s.mu.Unlock()
	return nil
}

// TogglePause 切换暂停/继续。
func (s *Scheduler) TogglePause() error {
	if s.state.Get().Paused {
		return s.Resume()
	}
	return s.Pause()
}

// Status 返回状态快照。
func (s *Scheduler) Status() Status {
	s.mu.Lock()
	lastErr := s.lastErr
	nextRun := s.nextRun
	s.mu.Unlock()
	st := s.state.Get()
	return Status{
		Paused:        st.Paused,
		NextRun:       nextRun,
		LastError:     lastErr,
		TaskInstalled: st.TaskInstalled,
		Fullscreen:    fullscreen.IsFullscreen(),
		Current:       st.Current,
		LockCurrent:   st.LockCurrent,
		History:       st.History,
		LockHistory:   st.LockHistory,
	}
}

func (s *Scheduler) setErr(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

// applyNext 完整执行一次换图: 取图 -> 下载 -> 应用 -> 记录历史 -> 清理缓存。
// 桌面与锁屏各自使用配置指定的数据源与预取队列, 同时启用时各取一张不同的图;
// 锁屏取图失败时降级为沿用桌面图。
// 通过 s.mu 串行化, 定时/手动调用不会并发执行。
func (s *Scheduler) applyNext() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	desktopItem := state.Item{}
	desktopOK := false

	if s.cfg.Targets.Desktop {
		item, err := s.takeItem(targetDesktop, "")
		if err != nil {
			return err
		}
		local, derr := s.downloadItem(item)
		if derr != nil {
			// 下载失败也标记已见, 避免坏地址反复阻塞后续换图
			s.markSeen(targetDesktop, item)
			return fmt.Errorf("下载壁纸失败: %w", derr)
		}
		item.Path = local
		if err := s.applyDesktop(item); err != nil {
			return err
		}
		desktopItem = item
		desktopOK = true
		s.markSeen(targetDesktop, item)
		s.applyTheme(item)
		_ = s.state.Update(func(u *state.State) {
			u.Current = item
			u.History = append([]state.Item{item}, u.History...)
			if len(u.History) > historyLimit {
				u.History = u.History[:historyLimit]
			}
			u.HistoryIndex = 0
		})
		s.log.Printf("desktop wallpaper changed: source=%s id=%s file=%s", item.Source, item.ID, item.Path)
	}

	lockItem := desktopItem
	if s.cfg.Targets.LockScreen {
		lockOK := false
		if desktopOK {
			// 锁屏单独取一张(与桌面不同)
			item, err := s.takeItem(targetLock, seenKeyOf(desktopItem))
			if err != nil {
				s.log.Printf("获取锁屏壁纸失败, 本次锁屏沿用桌面图: %v", err)
				lockOK = true
			} else {
				local, derr := s.downloadItem(item)
				if derr != nil {
					s.markSeen(targetLock, item)
					s.log.Printf("锁屏壁纸下载失败, 本次锁屏沿用桌面图: %v", derr)
					lockOK = true
				} else {
					item.Path = local
					lockItem = item
					lockOK = true
				}
			}
		} else {
			// 桌面未启用: 锁屏直接取
			item, err := s.takeItem(targetLock, "")
			if err != nil {
				return err
			}
			local, derr := s.downloadItem(item)
			if derr != nil {
				s.markSeen(targetLock, item)
				return fmt.Errorf("下载壁纸失败: %w", derr)
			}
			item.Path = local
			lockItem = item
			lockOK = true
		}
		if lockOK {
			if err := s.applyLockScreenItem(lockItem); err != nil {
				return err
			}
			s.markSeen(targetLock, lockItem)
			_ = s.state.Update(func(u *state.State) {
				u.LockCurrent = lockItem
				u.LockScreenTarget = lockItem.Path
				u.LockHistory = append([]state.Item{lockItem}, u.LockHistory...)
				if len(u.LockHistory) > historyLimit {
					u.LockHistory = u.LockHistory[:historyLimit]
				}
				u.LockHistoryIndex = 0
			})
			s.log.Printf("lock screen changed: source=%s id=%s file=%s", lockItem.Source, lockItem.ID, lockItem.Path)
		}
	}

	s.nextRun = time.Now().Add(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	s.cleanupCache()
	s.notifyRotation(desktopItem, lockItem)
	return nil
}

// applyTheme 按配置把壁纸主色联动到 Windows 主题(强调色/深浅色模式)。
// 失败只记日志, 不影响换图本身。
func (s *Scheduler) applyTheme(item state.Item) {
	if item.Path == "" {
		return
	}
	if !s.cfg.Theme.AccentFromWallpaper && !s.cfg.Theme.AutoLightDark {
		return
	}
	hex, err := theme.Apply(item.Path, s.cfg.Theme.AccentFromWallpaper, s.cfg.Theme.AutoLightDark)
	if err != nil {
		s.log.Printf("主题联动失败: %v", err)
		return
	}
	if hex != "" {
		s.log.Printf("theme synced from wallpaper: color=%s", hex)
	}
}

// notifyRotation 换图成功后发送 Windows 通知。
func (s *Scheduler) notifyRotation(desktop, lock state.Item) {
	var body string
	switch {
	case s.cfg.Targets.Desktop && s.cfg.Targets.LockScreen:
		body = "壁纸和锁屏更换成功"
	case s.cfg.Targets.Desktop:
		body = "桌面壁纸更换成功"
	case s.cfg.Targets.LockScreen:
		body = "锁屏壁纸更换成功"
	default:
		return
	}
	notify.Show("更换成功", body, s.log)
}

// 目标标识: 桌面与锁屏各自使用独立的数据源与预取队列。
const (
	targetDesktop = "desktop"
	targetLock    = "lock"
)

// cursorFor 取目标的预取队列状态。
func (s *Scheduler) cursorFor(target string) state.Cursor {
	st := s.state.Get()
	if target == targetLock {
		return st.Lock
	}
	return st.Desktop
}

// saveCursor 保存目标的预取队列状态。
func (s *Scheduler) saveCursor(target string, c state.Cursor) {
	_ = s.state.Update(func(u *state.State) {
		if target == targetLock {
			u.Lock = c
		} else {
			u.Desktop = c
		}
	})
}

// sourceFor 返回目标使用的数据源。
func (s *Scheduler) sourceFor(target string) source.Source {
	name := s.cfg.Sources.Desktop
	if target == targetLock {
		name = s.cfg.Sources.LockScreen
	}
	return s.sourceByName(name)
}

// sourceByName 按名字取数据源, 找不到时回退到 wallhaven。
func (s *Scheduler) sourceByName(name string) source.Source {
	if name == "" {
		name = config.SourceWallhaven
	}
	if sc, ok := s.sources[name]; ok {
		return sc
	}
	return s.sources[config.SourceWallhaven]
}

// seenKeyOf 生成跨源唯一的条目键(不同数据源的 ID 可能相同)。
func seenKeyOf(item state.Item) string {
	src := item.Source
	if src == "" {
		src = config.SourceWallhaven
	}
	return src + "/" + item.ID
}

// seenLimit 已看集合保留的最大条数。
// 超出后丢弃最旧的, 保证集合有界; 图源条目少于该值时会自动重置(洗牌袋)。
const seenLimit = 200

// isSeen 判断条目是否在目标的已看集合中。
func (s *Scheduler) isSeen(target, key string) bool {
	st := s.state.Get()
	for _, k := range st.Seen[target] {
		if k == key {
			return true
		}
	}
	return false
}

// markSeen 把条目记入目标的已看集合(持久化)。
// 重启后不会立刻重复旧图, 这是"一轮内不重复"的基础。
func (s *Scheduler) markSeen(target string, item state.Item) {
	key := seenKeyOf(item)
	_ = s.state.Update(func(u *state.State) {
		if u.Seen == nil {
			u.Seen = map[string][]string{}
		}
		list := u.Seen[target]
		for _, k := range list {
			if k == key {
				return
			}
		}
		list = append(list, key)
		if len(list) > seenLimit {
			list = list[len(list)-seenLimit:]
		}
		u.Seen[target] = list
	})
}

// resetSeen 清空目标的已看集合。
// 当图源条目已被看完时调用, 让下一轮重新洗牌, 而不是一直报"已全部看过"。
func (s *Scheduler) resetSeen(target string) {
	_ = s.state.Update(func(u *state.State) {
		if u.Seen != nil {
			delete(u.Seen, target)
		}
	})
	s.log.Printf("已看集合已满, %s 目标重新洗牌", target)
}

// takeItem 为目标取一条壁纸: 优先用预取队列, 队列空则向该目标的数据源拉一批。
// excludeKey 为需要排除的条目键, 用于保证桌面与锁屏不是同一张图。
// 调用方需持有 s.mu。
func (s *Scheduler) takeItem(target string, excludeKey string) (state.Item, error) {
	cur := s.cursorFor(target)
	src := s.sourceFor(target)
	queue := cur.Queue

	for len(queue) > 0 {
		cand := queue[0]
		queue = queue[1:]
		if cand.Source == "" {
			// 旧版本队列没有源标记, 按当前目标的数据源补全
			cand.Source = src.Name()
		}
		key := seenKeyOf(cand)
		if s.isSeen(target, key) || key == excludeKey {
			continue
		}
		cur.Queue = queue
		s.saveCursor(target, cur)
		return cand, nil
	}

	q := source.Query{
		Page:        cur.Page,
		Seed:        cur.Seed,
		Keyword:     s.cfg.Query,
		Orientation: s.cfg.Orientation,
	}
	if w, h, ok := parseAtLeast(s.cfg.AtLeast); ok {
		q.MinWidth, q.MinHeight = w, h
	}
	res, err := src.Search(q)
	if err != nil {
		return state.Item{}, err
	}
	newPage := cur.Page + 1
	newSeed := ""
	if res.LastPage > 0 && newPage > res.LastPage {
		newPage = 1 // 回到第一页, 丢弃旧 seed 让服务端生成新的随机序列
	} else {
		newSeed = res.Seed
	}
	cur.Page = newPage
	cur.Seed = newSeed
	seenSkipped := 0
	for _, it := range res.Items {
		cand := state.Item{
			ID:      it.ID,
			Source:  it.Source,
			FileURL: it.FileURL,
			PageURL: it.PageURL,
			Author:  it.Author,
			Title:   it.Title,
		}
		key := seenKeyOf(cand)
		if s.isSeen(target, key) {
			seenSkipped++
			continue
		}
		if key == excludeKey {
			continue
		}
		queue = append(queue, cand)
	}
	// 搜索到的图全都看过了 -> 说明这个图源已经被轮完一遍,
	// 清空已看集合重新洗牌, 而不是一直报"已全部看过"。
	if len(queue) == 0 && seenSkipped > 0 {
		s.resetSeen(target)
		for _, it := range res.Items {
			cand := state.Item{
				ID:      it.ID,
				Source:  it.Source,
				FileURL: it.FileURL,
				PageURL: it.PageURL,
				Author:  it.Author,
				Title:   it.Title,
			}
			if seenKeyOf(cand) == excludeKey {
				continue
			}
			queue = append(queue, cand)
		}
	}
	cur.Queue = queue
	if len(queue) == 0 {
		s.saveCursor(target, cur)
		return state.Item{}, errNoResults
	}
	item := queue[0]
	cur.Queue = queue[1:]
	s.saveCursor(target, cur)
	return item, nil
}

// downloadItem 按条目来源的数据源下载原图。
func (s *Scheduler) downloadItem(item state.Item) (string, error) {
	src := s.sourceByName(item.Source)
	if src == nil {
		return "", fmt.Errorf("未知数据源: %s", item.Source)
	}
	return src.Download(source.Item{
		ID:       item.ID,
		Source:   item.Source,
		FileURL:  item.FileURL,
		PageURL:  item.PageURL,
		FileType: fileExtGuess(item.FileURL),
		Title:    item.Title,
		Author:   item.Author,
	}, s.cacheDir())
}

// parseAtLeast 解析最小分辨率 "1920x1080"。
func parseAtLeast(s string) (int, int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// fileExtGuess 从直链 URL 猜测扩展名(wallhaven 直链以 .jpg/.png 结尾)。
func fileExtGuess(fileURL string) string {
	lower := strings.ToLower(fileURL)
	if strings.HasSuffix(lower, ".png") {
		return "image/png"
	}
	return "image/jpeg"
}

// applyDesktop 把本地图片应用到桌面。调用方需持有 s.mu。
func (s *Scheduler) applyDesktop(item state.Item) error {
	if err := wallpaper.SetDesktop(item.Path, s.cfg.DesktopFit); err != nil {
		return fmt.Errorf("设置桌面壁纸失败: %w", err)
	}
	return nil
}

// applyLockScreenItem 把本地图片写入锁屏目标文件并触发计划任务。调用方需持有 s.mu。
func (s *Scheduler) applyLockScreenItem(item state.Item) error {
	// 无论任务是否已安装都写目标文件, 便于 --install-task 安装后立即生效
	if err := os.WriteFile(paths.LockScreenTargetFile(), []byte(item.Path), 0o644); err != nil {
		return fmt.Errorf("写入锁屏目标文件失败: %w", err)
	}
	if s.state.Get().TaskInstalled {
		if err := elevate.RunTask(); err != nil {
			// 任务可能已被外部删除: 自愈重置标记, 降级为警告, 不中断本轮换图
			if ok, _ := elevate.TaskExists(); !ok {
				_ = s.state.Update(func(u *state.State) { u.TaskInstalled = false })
				s.log.Printf("锁屏计划任务不存在, 已重置安装标记(保存配置可重新安装): %v", err)
				return nil
			}
			return fmt.Errorf("触发锁屏计划任务失败: %w", err)
		}
		s.log.Printf("lock screen task triggered: %s", item.Path)
	}
	return nil
}

// cleanupCache 按修改时间清理缓存:
// 先删除超过 max_days 的文件(max_days=0 表示不限), 再保留最近 max_items 张;
// 当前桌面图与当前锁屏图永远不删。
func (s *Scheduler) cleanupCache() {
	st := s.state.Get()
	keep := map[string]bool{}
	if st.Current.Path != "" {
		keep[strings.ToLower(st.Current.Path)] = true
	}
	if st.LockCurrent.Path != "" {
		keep[strings.ToLower(st.LockCurrent.Path)] = true
	}
	if st.LockScreenTarget != "" {
		keep[strings.ToLower(st.LockScreenTarget)] = true
	}
	maxItems := s.cfg.Cache.MaxItems
	var cutoff time.Time
	if s.cfg.Cache.MaxDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -s.cfg.Cache.MaxDays)
	}

	cacheDir := s.cacheDir()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	type fileAge struct {
		path string
		mod  time.Time
	}
	var files []fileAge
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileAge{filepath.Join(cacheDir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	kept := 0
	for _, f := range files {
		if keep[strings.ToLower(f.path)] {
			continue
		}
		if !cutoff.IsZero() && f.mod.Before(cutoff) {
			if err := os.Remove(f.path); err != nil {
				s.log.Printf("清理过期缓存失败 %s: %v", f.path, err)
			} else {
				s.log.Printf("cache expired(%d days): %s", s.cfg.Cache.MaxDays, f.path)
			}
			continue
		}
		kept++
		if kept > maxItems {
			if err := os.Remove(f.path); err != nil {
				s.log.Printf("清理缓存失败 %s: %v", f.path, err)
			} else {
				s.log.Printf("cache cleaned: %s", f.path)
			}
		}
	}
}
