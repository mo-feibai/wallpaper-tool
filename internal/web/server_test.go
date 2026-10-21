package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wallpaper-tool/internal/scheduler"
	"wallpaper-tool/internal/state"
	"wallpaper-tool/internal/webimage"
)

// TestStatusResponseShape 确认状态接口的 JSON 不会出现重复键,
// 且外层的 current/history 覆盖嵌入结构中的同名字段。
func TestStatusResponseShape(t *testing.T) {
	resp := statusResponse{
		Status: scheduler.Status{
			Paused:        true,
			NextRun:       time.Now(),
			LastError:     "",
			TaskInstalled: true,
			Current:       state.Item{ID: "old-desktop"},
			LockCurrent:   state.Item{ID: "old-lock"},
			History:       []state.Item{{ID: "old-history"}},
			LockHistory:   []state.Item{{ID: "old-lock-history"}},
		},
		Current:     itemView{Item: state.Item{ID: "desktop-1"}},
		LockCurrent: itemView{Item: state.Item{ID: "lock-1"}},
		History:     []itemView{{Item: state.Item{ID: "h-1"}}},
		LockHistory: []itemView{{Item: state.Item{ID: "lh-1"}}},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 嵌入结构被遮蔽后, 不应再出现重复键; json.Unmarshal 取最后一个值,
	// 这里直接确认取到的是外层值。
	if !strings.Contains(string(raw["current"]), "desktop-1") {
		t.Errorf("current 应取外层值, 实际: %s", raw["current"])
	}
	if strings.Contains(string(raw["current"]), "old-desktop") {
		t.Errorf("current 不应包含嵌入结构的旧值: %s", raw["current"])
	}
	if !strings.Contains(string(raw["lock_current"]), "lock-1") {
		t.Errorf("lock_current 应取外层值, 实际: %s", raw["lock_current"])
	}
	for _, key := range []string{"paused", "next_run", "task_installed", "current", "lock_current", "history", "lock_history"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("缺少字段 %s", key)
		}
	}
}

// TestItemViewWebImageOmit 确认没有派生信息时 web_image 不出现在 JSON 里。
func TestItemViewWebImageOmit(t *testing.T) {
	data, err := json.Marshal(itemView{Item: state.Item{ID: "x"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "web_image") {
		t.Errorf("web_image 应被省略: %s", data)
	}
	data, err = json.Marshal(itemView{Item: state.Item{ID: "x"}, WebImage: &webimage.Info{State: webimage.StateReady}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "web_image") {
		t.Errorf("web_image 应出现: %s", data)
	}
}
