// Package suncalc 计算指定经纬度在指定日期的日出与日落时间。
//
// 采用 Meeus《天文算法》的日出方程(Wikipedia 上的 Sunrise equation):
//
//	n  = ceil(JD − 2451545.0 + 0.0008)              儒略日序数
//	J* = n − 经度/360
//	M  = (357.5291 + 0.98560028·J*) mod 360         太阳平近点角
//	C  = 1.9148·sinM + 0.02·sin2M + 0.0003·sin3M    中心差
//	λ  = (M + C + 180 + 102.9372) mod 360           视黄经
//	Jtransit = 2451545.0 + J* + 0.0053·sinM − 0.0069·sin2λ
//	cos ω0 = (sin(−0.833°) − sinφ·sinδ) / (cosφ·cosδ)   日出时角
//	Jrise = Jtransit − ω0/360      Jset = Jtransit + ω0/360
//
// 精度: 与 sunrise-sunset.org 的公开数据比对误差在 1 分钟以内。
// 极昼/极夜: 返回 ErrNoSunriseSunset, 由调用方决定回退策略。
//
// 时间语义: 产出 UTC 时刻, 调用方再换算到本机时区
// (要求本机时区与所设位置一致, 否则相差一个时区)。
package suncalc

import (
	"errors"
	"math"
	"time"
)

// ErrNoSunriseSunset 表示该日期在该位置没有日出或日落(极昼/极夜)。
var ErrNoSunriseSunset = errors.New("该位置当天没有日出或日落")

// 天文常量。
const (
	// sunElevation 日出时太阳中心的高度角(度)。
	// -0.833 = -(太阳视半径 0.267 + 大气折射 0.567)。
	sunElevation = -0.833
	// obliquity 黄赤交角(度), J2000 历元。
	obliquity = 23.4397
	// jdUnixEpoch 儒略日 2440587.5 对应 1970-01-01T00:00:00Z。
	jdUnixEpoch = 2440587.5
	rad         = math.Pi / 180
)

// Result 是一次日出日落计算的结果。
type Result struct {
	// Sunrise 当天的日出时刻(UTC)。
	Sunrise time.Time
	// Sunset 当天的日落时刻(UTC)。
	Sunset time.Time
	// DayLength 昼长。
	DayLength time.Duration
}

// Compute 计算 date 所在当地日期、指定经纬度的日出与日落(UTC)。
//
// lat 纬度(北正南负), lon 经度(东正西负)。
func Compute(date time.Time, lat, lon float64) (Result, error) {
	year, month, day := date.Date()
	// 用当天 UTC 午夜的儒略日: 公式的 n 以"日"为单位, 不能再叠加时刻小数
	midnight := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	jd := julianDay(midnight)

	// n 取当天及之后第一天的序数(公式要求 ceil, 保证日出落在当天)
	n := math.Ceil(jd - 2451545.0 + 0.0008)
	jStar := n - lon/360

	m := math.Mod(357.5291+0.98560028*jStar, 360)
	mRad := m * rad
	c := 1.9148*math.Sin(mRad) + 0.02*math.Sin(2*mRad) + 0.0003*math.Sin(3*mRad)

	lambda := math.Mod(m+c+180+102.9372, 360)
	lambdaRad := lambda * rad

	jTransit := 2451545.0 + jStar + 0.0053*math.Sin(mRad) - 0.0069*math.Sin(2*lambdaRad)

	// 太阳赤纬
	sinDelta := math.Sin(lambdaRad) * math.Sin(obliquity*rad)
	cosDelta := math.Cos(math.Asin(sinDelta))

	latRad := lat * rad
	cosOmega := (math.Sin(sunElevation*rad) - math.Sin(latRad)*sinDelta) / (math.Cos(latRad) * cosDelta)
	if cosOmega > 1 || cosOmega < -1 {
		// 无实数解: 该纬度当天极昼或极夜
		return Result{}, ErrNoSunriseSunset
	}
	omega := math.Acos(cosOmega) / rad // 度

	sunrise := julianToTime(jTransit - omega/360)
	sunset := julianToTime(jTransit + omega/360)
	if !sunset.After(sunrise) {
		return Result{}, ErrNoSunriseSunset
	}
	return Result{
		Sunrise:   sunrise,
		Sunset:    sunset,
		DayLength: sunset.Sub(sunrise),
	}, nil
}

// julianDay 把时刻转换成儒略日(含小数部分)。
func julianDay(t time.Time) float64 {
	t = t.UTC()
	y, m, d := t.Date()
	if m <= 2 {
		y--
		m += 12
	}
	a := math.Floor(float64(y) / 100)
	b := 2 - a + math.Floor(a/4)
	jd := math.Floor(365.25*float64(y+4716)) +
		math.Floor(30.6001*float64(m+1)) +
		float64(d) + b - 1524.5
	frac := float64(t.Hour())/24 + float64(t.Minute())/1440 + float64(t.Second())/86400
	return jd + frac
}

// julianToTime 把儒略日转换成 UTC 时刻。
func julianToTime(jd float64) time.Time {
	unix := (jd - jdUnixEpoch) * 86400
	return time.Unix(int64(math.Round(unix)), 0).UTC()
}
