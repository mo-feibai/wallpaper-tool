// Package web 提供本地 Web 配置服务(仅绑定 127.0.0.1)。
// 静态页面通过 go:embed 内嵌进二进制。
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"wallpaper-tool/internal/assets"
	"wallpaper-tool/internal/autostart"
	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/elevate"
	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/scheduler"
	"wallpaper-tool/internal/state"
)

//go:embed static
var staticFiles embed.FS

// Server Web 配置服务。
type Server struct {
	sched *scheduler.Scheduler
	state *state.Store
	log   *log.Logger
	ln    net.Listener
}

// New 创建服务。
func New(sched *scheduler.Scheduler, st *state.Store, lg *log.Logger) *Server {
	return &Server{sched: sched, state: st, log: lg}
}

// defaultPort 固定首选端口: 端口稳定后, 应用重启期间打开的配置页
// 在服务恢复后可通过轮询自动重连, 无需用户重新打开。
const defaultPort = 25317

// noCache 禁止静态资源被浏览器缓存。
// embed.FS 的 ModTime 为零值, FileServer 不会发 Last-Modified, 浏览器会启用
// 启发式缓存, 导致升级后配置页仍显示旧样式。
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

// Start 启动 HTTP 服务并返回实际绑定端口。
// 配置端口为 0 时优先使用固定默认端口(被其他程序占用则回退随机端口)。
func (s *Server) Start() (int, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handlePostConfig)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/next", s.handleNext)
	mux.HandleFunc("POST /api/prev", s.handlePrev)
	mux.HandleFunc("POST /api/pause", s.handlePause)
	mux.HandleFunc("POST /api/resume", s.handleResume)
	mux.HandleFunc("POST /api/lockscreen/remove", s.handleLockScreenRemove)
	mux.HandleFunc("GET /api/current-image", s.handleCurrentImage)
	mux.HandleFunc("GET /api/lockscreen-image", s.handleLockScreenImage)
	mux.HandleFunc("GET /api/history-image", s.handleHistoryImage)
	mux.HandleFunc("GET /assets/icon.png", s.handleAppIcon)

	sub, subErr := fs.Sub(staticFiles, "static")
	if subErr != nil {
		return 0, subErr
	}
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))

	port := s.sched.Config().Web.Port
	var ln net.Listener
	var err error
	if port == 0 {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
		if err != nil {
			s.log.Printf("default port %d occupied, falling back to random port", defaultPort)
			ln, err = net.Listen("tcp", "127.0.0.1:0")
		}
	} else {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	if err != nil {
		return 0, err
	}
	s.ln = ln
	actual := ln.Addr().(*net.TCPAddr).Port
	go func() {
		if err := http.Serve(ln, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Printf("web server: %v", err)
		}
	}()
	return actual, nil
}

// Stop 关闭 HTTP 服务。
func (s *Server) Stop() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, msg string, code int) {
	http.Error(w, msg, code)
}

// configPayload 是配置保存接口的载荷。
// UnsplashKey 为空表示"保持原值"(凭据不回显到前端);
// ClearUnsplashKey 为 true 表示清除已保存的 key。
type configPayload struct {
	config.Config
	ClearUnsplashKey bool `json:"clear_unsplash_key"`
}

// configResponse 是配置读取接口的响应。
// 不回显 Unsplash Access Key, 只告知是否已配置。
type configResponse struct {
	config.Config
	UnsplashKeySet bool `json:"unsplash_key_set"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.sched.Config()
	keySet := cfg.UnsplashKey != ""
	cfg.UnsplashKey = ""
	writeJSON(w, configResponse{Config: cfg, UnsplashKeySet: keySet})
}

// handlePostConfig 保存配置并处理副作用:
// 开机自启开关、锁屏计划任务首次安装(触发一次 UAC)。
func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	var payload configPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httpError(w, "配置格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	cfg := payload.Config
	cur := s.sched.Config()
	switch {
	case payload.ClearUnsplashKey:
		cfg.UnsplashKey = ""
	case cfg.UnsplashKey == "":
		// 前端不回显凭据, 空值表示保持原 key 不变
		cfg.UnsplashKey = cur.UnsplashKey
	}
	cfg.Sanitize()
	exe := paths.ExePath()

	// 副作用一: 开机自启
	if cfg.Autostart {
		if err := autostart.Enable(exe); err != nil {
			httpError(w, "设置开机自启失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := autostart.Disable(); err != nil {
			httpError(w, "关闭开机自启失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// 副作用二: 锁屏计划任务首次安装(runas 弹一次 UAC, 轮询确认任务创建成功)
	st := s.state.Get()
	if cfg.Targets.LockScreen && !st.TaskInstalled {
		if st.Current.Path != "" {
			if _, err := os.Stat(st.Current.Path); err == nil {
				_ = os.WriteFile(paths.LockScreenTargetFile(), []byte(st.Current.Path), 0o644)
			}
		}
		if err := elevate.RunElevated(exe, "--install-task"); err != nil {
			httpError(w, "启动提权安装失败(UAC 可能被取消): "+err.Error(), http.StatusInternalServerError)
			return
		}
		installed := false
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			if ok, _ := elevate.TaskExists(); ok {
				installed = true
				break
			}
		}
		if !installed {
			httpError(w, "等待计划任务创建超时, 请在 UAC 弹窗中允许后重试", http.StatusGatewayTimeout)
			return
		}
		_ = s.state.Update(func(u *state.State) { u.TaskInstalled = true })
		s.log.Printf("lock screen scheduled task installed")
	}

	// 乐观并发校验: 页面保存所基于的版本必须仍是当前版本,
	// 否则说明配置已被其他保存(或手工编辑)更新, 拒绝覆盖旧的表单快照
	if cfg.Revision != cur.Revision {
		httpError(w, "配置已在其他地方被修改, 请刷新页面后再保存", http.StatusConflict)
		return
	}
	cfg.Revision = cur.Revision + 1
	if err := s.sched.UpdateConfig(cfg); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 缓存目录变更时迁移旧图片(需在 UpdateConfig 之后, 使用新配置)
	if err := s.sched.SyncCacheDir(); err != nil {
		httpError(w, "缓存目录迁移失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	saved := s.sched.Config()
	keySet := saved.UnsplashKey != ""
	saved.UnsplashKey = ""
	writeJSON(w, configResponse{Config: saved, UnsplashKeySet: keySet})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.sched.Status()
	// 仅返回缓存文件仍存在的历史条目, 避免前端出现失效缩略图
	// (历史保留 50 条, 但缓存会按张数/天数清理旧图, 两者不必同步)
	filterExisting := func(items []state.Item) []state.Item {
		kept := make([]state.Item, 0, len(items))
		for _, it := range items {
			if it.Path == "" {
				continue
			}
			if _, err := os.Stat(it.Path); err != nil {
				continue
			}
			kept = append(kept, it)
		}
		return kept
	}
	st.History = filterExisting(st.History)
	st.LockHistory = filterExisting(st.LockHistory)
	writeJSON(w, st)
}

func (s *Server) handleNext(w http.ResponseWriter, r *http.Request) {
	if err := s.sched.Next(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePrev(w http.ResponseWriter, r *http.Request) {
	if err := s.sched.Prev(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if err := s.sched.Pause(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if err := s.sched.Resume(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleLockScreenRemove 提权移除锁屏策略(删除计划任务并清注册表)。
func (s *Server) handleLockScreenRemove(w http.ResponseWriter, r *http.Request) {
	exe := paths.ExePath()
	if err := elevate.RunElevated(exe, "--remove-task"); err != nil {
		httpError(w, "启动提权失败(UAC 可能被取消): "+err.Error(), http.StatusInternalServerError)
		return
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		if ok, _ := elevate.TaskExists(); !ok {
			break
		}
	}
	_ = s.state.Update(func(u *state.State) {
		u.TaskInstalled = false
		u.LockScreenTarget = ""
	})
	s.log.Printf("lock screen policy removed")
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleCurrentImage(w http.ResponseWriter, r *http.Request) {
	st := s.state.Get()
	if st.Current.Path == "" {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(st.Current.Path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, st.Current.Path)
}

// handleAppIcon 返回内嵌的应用图标(PNG), 供配置页 logo 与 favicon 使用。
func (s *Server) handleAppIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	_, _ = w.Write(assets.Png())
}

// handleHistoryImage 按壁纸 ID 在历史中查找并返回其本地图片。
// type=lock 时在锁屏历史中查找(桌面与锁屏历史独立存储)。
func (s *Server) handleHistoryImage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	st := s.state.Get()
	items := st.History
	if r.URL.Query().Get("type") == "lock" {
		items = st.LockHistory
	}
	for _, it := range items {
		if it.ID == id && it.Path != "" {
			if _, err := os.Stat(it.Path); err == nil {
				http.ServeFile(w, r, it.Path)
				return
			}
		}
	}
	http.NotFound(w, r)
}

// handleLockScreenImage 返回当前锁屏壁纸图片。
func (s *Server) handleLockScreenImage(w http.ResponseWriter, r *http.Request) {
	st := s.state.Get()
	if st.LockCurrent.Path == "" {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(st.LockCurrent.Path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, st.LockCurrent.Path)
}
