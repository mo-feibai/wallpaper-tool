package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/disintegration/imaging"
)

// TestWallpaperBrightnessDistribution 诊断工具: 打印一批壁纸的亮度分布,
// 用来核对深浅色判定规则是否与人的观感一致。
//
// 用法: WALLPAPER_DIR=<壁纸目录> go test ./internal/theme/ -run TestWallpaperBrightnessDistribution -v
func TestWallpaperBrightnessDistribution(t *testing.T) {
	dir := os.Getenv("WALLPAPER_DIR")
	if dir == "" {
		t.Skip("未设置 WALLPAPER_DIR")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	pngs, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	files = append(files, pngs...)
	sort.Strings(files)
	if len(files) == 0 {
		t.Skip("目录里没有图片")
	}
	fmt.Printf("%-28s %6s %6s %6s %6s %6s %6s\n", "file", "mean", "p50", "p70", "p75", "p90", "mode")
	for _, path := range files {
		img, err := imaging.Open(path, imaging.AutoOrientation(true))
		if err != nil {
			continue
		}
		small := imaging.Resize(img, 128, 128, imaging.Box)
		b := small.Bounds()
		lums := []float64{}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, a := small.At(x, y).RGBA()
				if a < 0x8000 {
					continue
				}
				lums = append(lums, pixelLuminance(int(r>>8), int(g>>8), int(bl>>8)))
			}
		}
		if len(lums) == 0 {
			continue
		}
		sort.Float64s(lums)
		pct := func(p float64) float64 {
			return lums[int(p*float64(len(lums)-1))]
		}
		sum := 0.0
		for _, l := range lums {
			sum += l
		}
		// 用新规则判定(以当前深色为起点)
		mode, changed := LightDarkDecision(pct(brightnessPercentile), 0)
		label := "亮"
		if mode == 0 {
			label = "暗"
		}
		if !changed {
			label = "保持"
		}
		fmt.Printf("%-28s %6.3f %6.3f %6.3f %6.3f %6.3f  %s\n",
			filepath.Base(path), sum/float64(len(lums)), pct(0.5), pct(0.7), pct(0.75), pct(0.9), label)
	}
}
