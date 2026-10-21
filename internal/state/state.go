// Package state 负责运行状态的持久化(state.json)。
// 包含当前壁纸、历史、预取队列、锁屏目标等。
package state

import (
	"encoding/json"
	"os"
	"sync"
)

// Item 表示一张壁纸。
type Item struct {
	ID      string `json:"id"`
	Source  string `json:"source,omitempty"`    // 数据源名(wallhaven/unsplash)
	FileURL string `json:"file_url,omitempty"`  // 远端原图直链
	Path    string `json:"path"`                // 本地缓存路径
	PageURL string `json:"page_url,omitempty"`  // 详情页地址
	Author  string `json:"author,omitempty"`    // 摄影师署名
	Title   string `json:"title,omitempty"`     // 标题或描述
}

// Cursor 记录某个目标的预取队列与分页状态。
// 桌面与锁屏各自维护一份, 因为两者可能使用不同数据源, 分页互不相干。
type Cursor struct {
	Queue []Item `json:"queue"`
	Page  int    `json:"page"`
	Seed  string `json:"seed"`
}

// State 顶层状态结构。
type State struct {
	Current          Item   `json:"current"`
	LockCurrent      Item   `json:"lock_current"` // 当前锁屏壁纸(可与桌面不同)
	History          []Item `json:"history"`      // 桌面壁纸历史
	HistoryIndex     int    `json:"history_index"`
	LockHistory      []Item `json:"lock_history"`      // 锁屏壁纸历史(独立存储)
	LockHistoryIndex int    `json:"lock_history_index"`
	Desktop          Cursor `json:"desktop"` // 桌面目标预取队列
	Lock             Cursor `json:"lock"`    // 锁屏目标预取队列
	// Seen 记录各目标最近使用过的条目键(源/ID), 按使用顺序排列(旧->新)。
	// 持久化后重启不会立刻重复旧图; 超过上限时丢弃最旧的。
	Seen map[string][]string `json:"seen,omitempty"`
	LockScreenTarget string `json:"lock_screen_target"`
	TaskInstalled    bool   `json:"task_installed"`
	Paused           bool   `json:"paused"`
	WebPort          int    `json:"web_port"`
	CacheDir         string `json:"cache_dir"` // 当前生效的缓存目录(用于目录变更时迁移)

	// 旧版本状态文件的扁平队列字段, 加载时迁移到 Desktop, 写回时不再输出。
	LegacyQueue []Item `json:"queue,omitempty"`
	LegacyPage  int    `json:"page,omitempty"`
	LegacySeed  string `json:"seed,omitempty"`
}

// Store 带锁的状态存储。
type Store struct {
	mu   sync.Mutex
	path string
	st   State
}

// Load 从磁盘加载状态。文件不存在时返回空状态。
// 解析失败时返回空状态与错误，由调用方记录。
func Load(path string) (*Store, error) {
	var st State
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &st); err != nil {
			return &Store{path: path}, err
		}
	} else if !os.IsNotExist(err) {
		return &Store{path: path}, err
	}
	if st.History == nil {
		st.History = []Item{}
	}
	if st.LockHistory == nil {
		st.LockHistory = []Item{}
	}
	// 旧版本把队列平铺在顶层, 迁移到桌面目标队列。
	if st.Desktop.Queue == nil && st.LegacyQueue != nil {
		st.Desktop = Cursor{Queue: st.LegacyQueue, Page: st.LegacyPage, Seed: st.LegacySeed}
	}
	st.LegacyQueue = nil
	st.LegacyPage = 0
	st.LegacySeed = ""
	if st.Desktop.Queue == nil {
		st.Desktop.Queue = []Item{}
	}
	if st.Lock.Queue == nil {
		st.Lock.Queue = []Item{}
	}
	return &Store{path: path, st: st}, nil
}

// Get 返回状态副本(切片做了深拷贝，避免并发问题)。
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.st
	c.History = append([]Item(nil), s.st.History...)
	c.LockHistory = append([]Item(nil), s.st.LockHistory...)
	c.Desktop.Queue = append([]Item(nil), s.st.Desktop.Queue...)
	c.Lock.Queue = append([]Item(nil), s.st.Lock.Queue...)
	if s.st.Seen != nil {
		c.Seen = make(map[string][]string, len(s.st.Seen))
		for k, v := range s.st.Seen {
			c.Seen[k] = append([]string(nil), v...)
		}
	}
	return c
}

// Update 在锁内修改状态并立即持久化。
func (s *Store) Update(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.st)
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
