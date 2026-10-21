// Package theme 实现"壁纸取色 -> 联动 Windows 主题"。
//
// 思路: 从壁纸中提取主色, 写入 Windows 的强调色(AccentColor),
// 并可按壁纸明暗自动切换系统的深/浅色模式。
package theme

import (
	"fmt"
	"image/color"

	"github.com/disintegration/imaging"
)

// sampleSize 取色时的采样边长(正方形缩略)。
// 取主色不需要全分辨率, 64x64 已足够代表画面主调。
const sampleSize = 64

// Dominant 从图片中提取主色。
//
// 做法: 先缩到 64x64, 再按 4bit/通道分桶统计出现次数,
// 取出现最多的桶, 再用该桶内像素求均值, 避免量化误差。
func Dominant(path string) (color.RGBA, error) {
	img, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return color.RGBA{}, fmt.Errorf("打开图片失败: %w", err)
	}
	small := imaging.Resize(img, sampleSize, sampleSize, imaging.Box)
	bounds := small.Bounds()

	// 计数桶: key = r>>4<<8 | g>>4<<4 | b>>4
	const buckets = 4096
	counts := make([]int, buckets)
	sumR := make([]uint64, buckets)
	sumG := make([]uint64, buckets)
	sumB := make([]uint64, buckets)
	total := 0

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
			total++
		}
	}
	if total == 0 {
		return color.RGBA{}, fmt.Errorf("图片没有可用像素")
	}

	best, bestCount := 0, -1
	for i, c := range counts {
		if c > bestCount {
			best, bestCount = i, c
		}
	}
	n := uint64(bestCount)
	return color.RGBA{
		R: uint8(sumR[best] / n),
		G: uint8(sumG[best] / n),
		B: uint8(sumB[best] / n),
		A: 255,
	}, nil
}

// Luminance 返回感知亮度(0~1)。
// 用 ITU-R BT.601 的加权公式, 与人眼对亮度的感受接近。
func Luminance(c color.RGBA) float64 {
	return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
}

// ABGR 返回 Windows 强调色格式(0xAABBGGRR)。
// 注册表里的 AccentColor 是 ABGR 顺序, 与常见的 RGB 相反。
func ABGR(c color.RGBA) uint32 {
	return uint32(c.A)<<24 | uint32(c.B)<<16 | uint32(c.G)<<8 | uint32(c.R)
}
