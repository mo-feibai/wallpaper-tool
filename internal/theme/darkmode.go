package theme

import (
	"fmt"
	"time"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/suncalc"
)

// 深浅色模式取值。
const (
	// ModeDark 深色。
	ModeDark = 0
	// ModeLight 浅色。
	ModeLight = 1
)

// DarkModeStatus 是一次深浅色模式判断的结果。
type DarkModeStatus struct {
	// Enabled 功能是否启用(配置为 off 时为 false)。
	Enabled bool `json:"enabled"`
	// Mode 当前应当使用的模式(ModeDark / ModeLight)。
	Mode int `json:"mode"`
	// Sunrise 太阳模式下的日出时刻(本地时区), 固定时段模式为空。
	Sunrise time.Time `json:"sunrise,omitempty"`
	// Sunset 太阳模式下的日落时刻(本地时区), 固定时段模式为空。
	Sunset time.Time `json:"sunset,omitempty"`
	// NextChange 下次切换时刻(本地时区), 零值表示本次不再变化。
	NextChange time.Time `json:"next_change,omitempty"`
	// Reason 判断依据的可读描述, 用于页面展示。
	Reason string `json:"reason,omitempty"`
}

// DesiredMode 根据配置计算此刻应使用的深浅色模式。
//
// 固定时段: [day_start, night_start) 之间为浅色, 支持跨午夜。
// 太阳升落: [日出+dawn_offset, 日落-dusk_offset) 之间为浅色。
func DesiredMode(now time.Time, cfg config.DarkMode) (DarkModeStatus, error) {
	st := DarkModeStatus{}
	switch cfg.Mode {
	case config.DarkModeOff, "":
		return st, nil
	case config.DarkModeFixed:
		return desiredFixed(now, cfg)
	case config.DarkModeSun:
		return desiredSun(now, cfg)
	default:
		return st, fmt.Errorf("未知的深浅色模式: %s", cfg.Mode)
	}
}

// desiredFixed 按固定时段判断。
func desiredFixed(now time.Time, cfg config.DarkMode) (DarkModeStatus, error) {
	dayStart, err := parseClock(cfg.DayStart)
	if err != nil {
		return DarkModeStatus{}, fmt.Errorf("白天开始时间无效: %w", err)
	}
	nightStart, err := parseClock(cfg.NightStart)
	if err != nil {
		return DarkModeStatus{}, fmt.Errorf("夜晚开始时间无效: %w", err)
	}
	cur := now.Hour()*60 + now.Minute()
	day, night := dayStart, nightStart

	light := false
	switch {
	case day < night:
		light = cur >= day && cur < night
	case day > night:
		// 跨午夜: 例如 22:00 ~ 06:00
		light = cur >= day || cur < night
	default:
		light = true
	}

	st := DarkModeStatus{Enabled: true, Mode: ModeDark}
	if light {
		st.Mode = ModeLight
	}
	st.Reason = fmt.Sprintf("固定时段 %s ~ %s", cfg.DayStart, cfg.NightStart)
	// 下次切换
	if light {
		st.NextChange = time.Date(now.Year(), now.Month(), now.Day(), night/60, night%60, 0, 0, now.Location())
	} else {
		st.NextChange = time.Date(now.Year(), now.Month(), now.Day(), day/60, day%60, 0, 0, now.Location())
	}
	if !st.NextChange.After(now) {
		st.NextChange = st.NextChange.AddDate(0, 0, 1)
	}
	return st, nil
}

// desiredSun 按日出日落判断。
func desiredSun(now time.Time, cfg config.DarkMode) (DarkModeStatus, error) {
	res, err := suncalc.Compute(now, cfg.Latitude, cfg.Longitude)
	if err != nil {
		return DarkModeStatus{}, err
	}
	sunrise := res.Sunrise.In(now.Location())
	sunset := res.Sunset.In(now.Location())

	lightStart := sunrise.Add(time.Duration(cfg.DawnOffset) * time.Minute)
	lightEnd := sunset.Add(-time.Duration(cfg.DuskOffset) * time.Minute)

	st := DarkModeStatus{
		Enabled: true,
		Mode:    ModeDark,
		Sunrise: sunrise,
		Sunset:  sunset,
		Reason: fmt.Sprintf("日出 %s / 日落 %s",
			sunrise.Format("15:04"), sunset.Format("15:04")),
	}
	if now.After(lightStart) && now.Before(lightEnd) {
		st.Mode = ModeLight
		st.NextChange = lightEnd
	} else {
		st.NextChange = lightStart
	}
	if !st.NextChange.After(now) {
		// 跨日: 落到明天
		st.NextChange = st.NextChange.AddDate(0, 0, 1)
	}
	return st, nil
}

// parseClock 解析 "HH:MM" 形式的时刻, 返回当天第几分钟。
func parseClock(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("格式应为 HH:MM")
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("超出范围")
	}
	return h*60 + m, nil
}
