package theme

import (
	"testing"
	"time"

	"wallpaper-tool/internal/config"
)

// TestDesiredModeFixed 验证固定时段模式: 白天区间为浅色, 夜间为深色。
func TestDesiredModeFixed(t *testing.T) {
	loc := time.FixedZone("T", 8*3600)
	cfg := config.DarkMode{
		Mode:       config.DarkModeFixed,
		DayStart:   "08:00",
		NightStart: "19:00",
	}
	cases := []struct {
		hour int
		want int
	}{
		{7, ModeDark},   // 早上 7 点 -> 深色
		{9, ModeLight},  // 上午 9 点 -> 浅色
		{12, ModeLight}, // 中午 -> 浅色
		{18, ModeLight}, // 傍晚 18 点 -> 浅色
		{20, ModeDark},  // 晚上 20 点 -> 深色
		{3, ModeDark},   // 凌晨 -> 深色
	}
	for _, c := range cases {
		now := time.Date(2026, 10, 21, c.hour, 0, 0, 0, loc)
		st, err := DesiredMode(now, cfg)
		if err != nil {
			t.Fatalf("desired: %v", err)
		}
		if st.Mode != c.want {
			t.Errorf("%d 点: 模式 = %d, 期望 %d", c.hour, st.Mode, c.want)
		}
	}
}

// TestDesiredModeFixedCrossMidnight 验证跨午夜的固定时段(如 22:00 ~ 06:00)。
func TestDesiredModeFixedCrossMidnight(t *testing.T) {
	loc := time.FixedZone("T", 8*3600)
	cfg := config.DarkMode{
		Mode:       config.DarkModeFixed,
		DayStart:   "22:00",
		NightStart: "06:00",
	}
	cases := []struct {
		hour int
		want int
	}{
		{23, ModeLight}, // 23 点在区间内
		{2, ModeLight},  // 凌晨 2 点也在区间内
		{12, ModeDark},  // 中午不在区间内
	}
	for _, c := range cases {
		now := time.Date(2026, 10, 21, c.hour, 0, 0, 0, loc)
		st, err := DesiredMode(now, cfg)
		if err != nil {
			t.Fatalf("desired: %v", err)
		}
		if st.Mode != c.want {
			t.Errorf("%d 点: 模式 = %d, 期望 %d", c.hour, st.Mode, c.want)
		}
	}
}

// TestDesiredModeSun 验证太阳模式: 白天为浅色, 夜晚为深色。
func TestDesiredModeSun(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	cfg := config.DarkMode{
		Mode:      config.DarkModeSun,
		Latitude:  39.9042,
		Longitude: 116.4074,
	}
	// 2026-10-21 北京: 日出约 06:30, 日落约 17:27 (本地时区)
	noon := time.Date(2026, 10, 21, 12, 0, 0, 0, loc)
	st, err := DesiredMode(noon, cfg)
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if st.Mode != ModeLight {
		t.Errorf("中午模式 = %d, 期望浅色", st.Mode)
	}
	if st.Sunrise.Hour() != 6 || st.Sunset.Hour() != 17 {
		t.Errorf("日出 %v / 日落 %v, 期望约 06:30 / 17:27", st.Sunrise, st.Sunset)
	}
	if !st.NextChange.After(noon) {
		t.Errorf("下次切换 %v 应晚于当前时刻", st.NextChange)
	}

	night := time.Date(2026, 10, 21, 22, 0, 0, 0, loc)
	st2, err := DesiredMode(night, cfg)
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if st2.Mode != ModeDark {
		t.Errorf("夜间模式 = %d, 期望深色", st2.Mode)
	}
}

// TestDesiredModeOff 验证关闭模式时不启用。
func TestDesiredModeOff(t *testing.T) {
	st, err := DesiredMode(time.Now(), config.DarkMode{Mode: config.DarkModeOff})
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if st.Enabled {
		t.Error("关闭模式下 Enabled 应为 false")
	}
}
