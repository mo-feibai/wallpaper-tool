package suncalc

import (
	"math"
	"testing"
	"time"
)

// TestSunTimesAgainstReference 用 sunrise-sunset.org 的公开数据核对算法。
//
// 参考值(UTC):
//
//	北京 2026-01-01  日出 2025-12-31T23:35:48Z  日落 2026-01-01T08:59:46Z
//	北京 2026-06-21  日出 2026-06-20T20:45:56Z  日落 2026-06-21T11:46:19Z
//	北京 2026-03-20  日出 2026-03-19T22:18:03Z  日落 2026-03-20T10:25:47Z
//	伦敦 2026-03-20  日出 2026-03-20T06:02:42Z  日落 2026-03-20T18:13:13Z
//	赤道 2026-03-20  日出 2026-03-20T06:04:03Z  日落 2026-03-20T10:25:47Z 附近
//	悉尼 2026-12-21  日出 2026-12-20T18:40:40Z  日落 2026-12-21T09:05:22Z
func TestSunTimesAgainstReference(t *testing.T) {
	cases := []struct {
		name     string
		date     string
		lat, lon float64
		sunrise  string
		sunset   string
	}{
		{"北京冬至", "2026-01-01", 39.9042, 116.4074, "2025-12-31T23:35:48Z", "2026-01-01T08:59:46Z"},
		{"北京夏至", "2026-06-21", 39.9042, 116.4074, "2026-06-20T20:45:56Z", "2026-06-21T11:46:19Z"},
		{"北京春分", "2026-03-20", 39.9042, 116.4074, "2026-03-19T22:18:03Z", "2026-03-20T10:25:47Z"},
		{"伦敦春分", "2026-03-20", 51.5074, -0.1278, "2026-03-20T06:02:42Z", "2026-03-20T18:13:13Z"},
		{"赤道春分", "2026-03-20", 0, 0, "2026-03-20T06:04:03Z", "2026-03-20T18:10:51Z"},
		{"悉尼夏至", "2026-12-21", -33.8688, 151.2093, "2026-12-20T18:40:40Z", "2026-12-21T09:05:22Z"},
	}
	const tol = 2 * time.Minute
	for _, c := range cases {
		date, err := time.ParseInLocation("2006-01-02", c.date, time.UTC)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		res, err := Compute(date, c.lat, c.lon)
		if err != nil {
			t.Errorf("%s: 计算失败: %v", c.name, err)
			continue
		}
		wantRise, _ := time.Parse(time.RFC3339, c.sunrise)
		wantSet, _ := time.Parse(time.RFC3339, c.sunset)
		if d := res.Sunrise.UTC().Sub(wantRise); d < -tol || d > tol {
			t.Errorf("%s: 日出 = %s, 期望 %s (差 %v)", c.name, res.Sunrise.UTC(), wantRise, d)
		}
		if d := res.Sunset.UTC().Sub(wantSet); d < -tol || d > tol {
			t.Errorf("%s: 日落 = %s, 期望 %s (差 %v)", c.name, res.Sunset.UTC(), wantSet, d)
		}
	}
}

// TestPolarDayNight 验证极昼/极夜会被识别出来而不是算出错误时间。
func TestPolarDayNight(t *testing.T) {
	loc := time.FixedZone("T", 8*3600)
	// 北极圈内 6 月: 极昼
	date := time.Date(2026, 6, 21, 12, 0, 0, 0, loc)
	if _, err := Compute(date, 80.0, 0); err == nil {
		t.Error("北极圈 6 月应返回无日出日落")
	}
	// 北极圈内 12 月: 极夜
	date2 := time.Date(2026, 12, 21, 12, 0, 0, 0, loc)
	if _, err := Compute(date2, 80.0, 0); err == nil {
		t.Error("北极圈 12 月应返回无日出日落")
	}
}

// TestDayLength 验证昼长: 春分日全球昼夜基本各半。
func TestDayLength(t *testing.T) {
	date := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	res, err := Compute(date, 0, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	hours := res.DayLength.Hours()
	if math.Abs(hours-12) > 0.5 {
		t.Errorf("春分日赤道昼长 = %.2f 小时, 期望约 12 小时", hours)
	}
}

// TestLocalConversion 验证按本机时区换算: 北京(UTC+8)冬至日出应为 07:35 前后。
// Compute 返回 UTC 时刻, 由调用方换算到本机时区。
func TestLocalConversion(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	date := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	res, err := Compute(date, 39.9042, 116.4074)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if got := res.Sunrise.In(loc).Format("15:04"); got != "07:35" {
		t.Errorf("北京冬至日出 = %s, 期望 07:35", got)
	}
	if got := res.Sunset.In(loc).Format("15:04"); got != "16:59" {
		t.Errorf("北京冬至日落 = %s, 期望 16:59", got)
	}
}
