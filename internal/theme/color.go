// Package theme 实现"壁纸取色 -> 联动 Windows 主题"。
//
// 思路: 开启 Windows 原生的"从背景自动选取主题色"(Themes\History\AutoColor),
// 由系统自己从壁纸提取强调色; 深浅色模式则由本包按整图亮度判断后写入。
package theme

import (
	"fmt"
	"image/color"
	"sort"

	"github.com/disintegration/imaging"
)

// sampleSize 取色时的采样边长(正方形缩略)。
// 取主色不需要全分辨率, 64x64 已足够代表画面主调。
const sampleSize = 64

// lightThreshold 深浅色分界亮度。
//
// 判定量是 p75 分位数(见 Analysis.Brightness), 天然高于均值/中位数,
// 所以阈值要比直觉的 0.5 低, 实测 0.4 能把"大面积亮天空 + 深色前景剪影"
// 这类黄昏图正确判为亮色。
const lightThreshold = 0.40

// hysteresis 迟滞带宽: 亮度落在 lightThreshold±带宽 内时不切换深浅色。
//
// 没有迟滞时, 亮度在阈值附近来回的壁纸会让整个系统界面反复翻转, 非常刺眼。
const hysteresis = 0.08

// brightnessPercentile 亮度判定所用的分位数(0~1)。
const brightnessPercentile = 0.75

// Analysis 是壁纸的颜色分析结果。
type Analysis struct {
	// Dominant 主色(出现最多的颜色桶的均值)。
	Dominant color.RGBA
	// Brightness 整图亮度的 p75 分位数(0~1), 用于判断深/浅色模式。
	//
	// 用 p75 而不是均值/中位数:
	//   - 均值会被局部高光拉高(夜景里一块很亮的天空就能把均值抬到中灰以上)
	//   - 中位数会忽略大面积亮区(黄昏图: 上半深蓝夜空, 中间一条落日亮带,
	//     中位数落在暗区, 与"这张图看起来是亮的"直觉相反)
	// p75 既不被少量高光干扰, 又能反映占四分之一以上的亮区。
	Brightness float64
}

// Analyze 分析壁纸的主色与整图亮度。
//
// 一次解码同时产出两个结果, 不额外付出解码成本。
func Analyze(path string) (Analysis, error) {
	img, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return Analysis{}, fmt.Errorf("打开图片失败: %w", err)
	}
	small := imaging.Resize(img, sampleSize, sampleSize, imaging.Box)
	bounds := small.Bounds()

	// 计数桶: key = r>>4<<8 | g>>4<<4 | b>>4
	const buckets = 4096
	counts := make([]int, buckets)
	sumR := make([]uint64, buckets)
	sumG := make([]uint64, buckets)
	sumB := make([]uint64, buckets)

	// 每个采样点的亮度, 用于求中位数
	lums := make([]float64, 0, sampleSize*sampleSize)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := small.At(x, y).RGBA()
			if a < 0x8000 {
				continue // 透明像素不参与
			}
			r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
			idx := (r8>>4)<<8 | (g8>>4)<<4 | (b8 >> 4)
			counts[idx]++
			sumR[idx] += uint64(r8)
			sumG[idx] += uint64(g8)
			sumB[idx] += uint64(b8)
			lums = append(lums, pixelLuminance(r8, g8, b8))
		}
	}
	if len(lums) == 0 {
		return Analysis{}, fmt.Errorf("图片没有可用像素")
	}

	best, bestCount := 0, -1
	for i, c := range counts {
		if c > bestCount {
			best, bestCount = i, c
		}
	}
	n := uint64(bestCount)
	dom := color.RGBA{
		R: uint8(sumR[best] / n),
		G: uint8(sumG[best] / n),
		B: uint8(sumB[best] / n),
		A: 255,
	}

	sort.Float64s(lums)
	brightness := lums[int(brightnessPercentile*float64(len(lums)-1))]

	return Analysis{Dominant: dom, Brightness: brightness}, nil
}

// Dominant 从图片中提取主色(只关心颜色时用这个)。
func Dominant(path string) (color.RGBA, error) {
	a, err := Analyze(path)
	if err != nil {
		return color.RGBA{}, err
	}
	return a.Dominant, nil
}

// Luminance 返回感知亮度(0~1)。
// 用 ITU-R BT.601 的加权公式, 与人眼对亮度的感受接近。
func Luminance(c color.RGBA) float64 {
	return pixelLuminance(int(c.R), int(c.G), int(c.B))
}

// pixelLuminance 计算 8bit RGB 的感知亮度(0~1)。
func pixelLuminance(r, g, b int) float64 {
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
}

// LightDarkDecision 纯函数: 根据整图亮度与当前模式, 决定是否切换深/浅色。
//
// brightness 整图亮度 p75 分位数(0~1)
// current  当前模式(1=浅色, 0=深色)
// 返回目标模式与是否真的需要切换。
//
// 规则:
//  1. 目标模式 = 亮度 >= lightThreshold ? 浅 : 深
//  2. 目标与当前一致 -> 不切换
//  3. 亮度落在阈值±迟滞带内 -> 保持当前模式, 避免界面反复翻转
func LightDarkDecision(brightness float64, current int) (target int, changed bool) {
	target = 0
	if brightness >= lightThreshold {
		target = 1
	}
	if target == current {
		return current, false
	}
	if d := brightness - lightThreshold; d <= hysteresis && d >= -hysteresis {
		return current, false
	}
	return target, true
}
