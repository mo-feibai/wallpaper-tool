package scheduler

import (
	"io"
	"log"
	"path/filepath"
	"testing"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/source"
	"wallpaper-tool/internal/state"
)

// fakeSource 是测试用的假数据源, 返回固定的一批条目。
type fakeSource struct {
	name  string
	items []source.Item
	// failSearch 为 true 时 Search 直接报错。
	failSearch bool
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Search(q source.Query) (source.Result, error) {
	if f.failSearch {
		return source.Result{}, errNoResults
	}
	return source.Result{Items: f.items}, nil
}

func (f *fakeSource) Download(item source.Item, destDir string) (string, error) {
	return filepath.Join(destDir, item.ID), nil
}

func (f *fakeSource) Update(cfg config.Config) {}

// newTestScheduler 构造一个使用临时状态文件的调度器。
func newTestScheduler(t *testing.T, cfg config.Config, srcs map[string]source.Source) (*Scheduler, *state.Store) {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	lg := log.New(io.Discard, "", 0)
	return New(cfg, st, srcs, lg), st
}

func fakeItems(ids ...string) []source.Item {
	items := make([]source.Item, 0, len(ids))
	for _, id := range ids {
		items = append(items, source.Item{ID: id, Source: config.SourceWallhaven, FileURL: "http://x/" + id})
	}
	return items
}

// TestTakeItemSkipsSeen 验证已看过的条目不会被再次取出。
func TestTakeItemSkipsSeen(t *testing.T) {
	cfg := config.Default()
	fs := &fakeSource{name: config.SourceWallhaven, items: fakeItems("a", "b", "c")}
	s, _ := newTestScheduler(t, cfg, map[string]source.Source{config.SourceWallhaven: fs})

	// 先把 "a" 标记为已看
	s.markSeen(targetDesktop, state.Item{ID: "a", Source: config.SourceWallhaven})

	for i := 0; i < 3; i++ {
		item, err := s.takeItem(targetDesktop, "")
		if err != nil {
			t.Fatalf("takeItem %d: %v", i, err)
		}
		if item.ID == "a" {
			t.Fatalf("取到了已看过的条目 a")
		}
	}
}

// TestShuffleBagResetsWhenExhausted 验证图源被轮完后重新洗牌,
// 而不是一直报"已全部看过"。
func TestShuffleBagResetsWhenExhausted(t *testing.T) {
	cfg := config.Default()
	fs := &fakeSource{name: config.SourceWallhaven, items: fakeItems("a", "b", "c")}
	s, _ := newTestScheduler(t, cfg, map[string]source.Source{config.SourceWallhaven: fs})

	// 把三条全部看完
	for _, id := range []string{"a", "b", "c"} {
		s.markSeen(targetDesktop, state.Item{ID: id, Source: config.SourceWallhaven})
	}
	// 再取时应触发重置并成功返回
	item, err := s.takeItem(targetDesktop, "")
	if err != nil {
		t.Fatalf("洗牌后仍取不到: %v", err)
	}
	if item.ID == "" {
		t.Fatalf("洗牌后返回了空条目")
	}
	// 重置后已看集合应被清空(条目是在应用成功后才标记已看)
	st := s.state.Get()
	if len(st.Seen[targetDesktop]) != 0 {
		t.Fatalf("洗牌后已看集合长度 = %d, 期望 0", len(st.Seen[targetDesktop]))
	}
	// 应用成功后标记已看, 集合应重新增长
	s.markSeen(targetDesktop, item)
	st = s.state.Get()
	if len(st.Seen[targetDesktop]) != 1 {
		t.Fatalf("标记后已看集合长度 = %d, 期望 1", len(st.Seen[targetDesktop]))
	}
}

// TestSeenPersistsAcrossScheduler 验证已看集合持久化: 换一个新调度器仍然记得。
func TestSeenPersistsAcrossScheduler(t *testing.T) {
	cfg := config.Default()
	fs := &fakeSource{name: config.SourceWallhaven, items: fakeItems("a")}
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := state.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lg := log.New(io.Discard, "", 0)
	s1 := New(cfg, st, map[string]source.Source{config.SourceWallhaven: fs}, lg)
	s1.markSeen(targetDesktop, state.Item{ID: "a", Source: config.SourceWallhaven})

	// 用同一个状态文件重新加载, 模拟重启
	st2, err := state.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	s2 := New(cfg, st2, map[string]source.Source{config.SourceWallhaven: fs}, lg)
	if !s2.isSeen(targetDesktop, "wallhaven/a") {
		t.Fatalf("重启后丢失了已看集合")
	}
}

// TestMarkSeenTrimsToLimit 验证已看集合有界。
func TestMarkSeenTrimsToLimit(t *testing.T) {
	cfg := config.Default()
	fs := &fakeSource{name: config.SourceWallhaven}
	s, _ := newTestScheduler(t, cfg, map[string]source.Source{config.SourceWallhaven: fs})

	for i := 0; i < seenLimit+50; i++ {
		s.markSeen(targetDesktop, state.Item{ID: string(rune('a'+i%26)) + itoa(i), Source: config.SourceWallhaven})
	}
	st := s.state.Get()
	if len(st.Seen[targetDesktop]) != seenLimit {
		t.Fatalf("已看集合长度 = %d, 期望 %d", len(st.Seen[targetDesktop]), seenLimit)
	}
}

// TestSourcePerTarget 验证桌面与锁屏各自使用配置指定的数据源。
func TestSourcePerTarget(t *testing.T) {
	cfg := config.Default()
	cfg.Sources.Desktop = config.SourceWallhaven
	cfg.Sources.LockScreen = config.SourceUnsplash
	wh := &fakeSource{name: config.SourceWallhaven, items: fakeItems("a")}
	un := &fakeSource{name: config.SourceUnsplash, items: fakeItems("b")}
	s, _ := newTestScheduler(t, cfg, map[string]source.Source{
		config.SourceWallhaven: wh,
		config.SourceUnsplash:  un,
	})

	if got := s.sourceFor(targetDesktop).Name(); got != config.SourceWallhaven {
		t.Errorf("桌面数据源 = %s, 期望 %s", got, config.SourceWallhaven)
	}
	if got := s.sourceFor(targetLock).Name(); got != config.SourceUnsplash {
		t.Errorf("锁屏数据源 = %s, 期望 %s", got, config.SourceUnsplash)
	}
}

// TestSearchSignatureDetectsChange 验证筛选条件变化会改变签名(触发清队列)。
func TestSearchSignatureDetectsChange(t *testing.T) {
	cfg := config.Default()
	base := searchSignature(cfg)
	cfg.Query = "forest"
	if searchSignature(cfg) == base {
		t.Errorf("关键词变化后签名未变")
	}
	cfg = config.Default()
	cfg.Orientation = "portrait"
	if searchSignature(cfg) == base {
		t.Errorf("方向变化后签名未变")
	}
}

// TestTakeItemPropagatesSourceError 验证数据源报错时透传错误。
func TestTakeItemPropagatesSourceError(t *testing.T) {
	cfg := config.Default()
	fs := &fakeSource{name: config.SourceWallhaven, failSearch: true}
	s, _ := newTestScheduler(t, cfg, map[string]source.Source{config.SourceWallhaven: fs})
	if _, err := s.takeItem(targetDesktop, ""); err == nil {
		t.Fatalf("数据源报错时应返回错误")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
