package theme

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/disintegration/imaging"
)

// TestDominantFindsMainColor 验证主色提取: 纯红图应提取出红色。
func TestDominantFindsMainColor(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/red.png"
	img := imaging.New(200, 100, color.NRGBA{R: 220, G: 30, B: 40, A: 255})
	if err := imaging.Save(img, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	c, err := Dominant(path)
	if err != nil {
		t.Fatalf("dominant: %v", err)
	}
	if c.R < 180 || c.G > 80 || c.B > 80 {
		t.Errorf("主色 = %v, 期望接近红色", c)
	}
}

// TestLuminance 验证亮度计算: 黑色为 0, 白色为 1。
func TestLuminance(t *testing.T) {
	if l := Luminance(color.RGBA{A: 255}); l > 0.001 {
		t.Errorf("黑色亮度 = %v, 期望 0", l)
	}
	if l := Luminance(color.RGBA{R: 255, G: 255, B: 255, A: 255}); l < 0.999 {
		t.Errorf("白色亮度 = %v, 期望 1", l)
	}
}

// TestDominantOnRealWallpaper 打印实际壁纸的主色, 便于人工核对。
func TestDominantOnRealWallpaper(t *testing.T) {
	path := os.Getenv("WALLPAPER_SAMPLE")
	if path == "" {
		t.Skip("未设置 WALLPAPER_SAMPLE")
	}
	c, err := Dominant(path)
	if err != nil {
		t.Fatalf("dominant: %v", err)
	}
	fmt.Printf("主色 = #%02X%02X%02X 亮度=%.3f\n", c.R, c.G, c.B, Luminance(c))
	var _ image.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
}

// TestLightDarkEndToEnd 端到端验证: 合成图 -> 整图亮度 -> 深浅色判决。
func TestLightDarkEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name       string
		gray       uint8
		current    int
		wantMode   int
		wantChange bool
	}{
		{"暗图, 当前深色", 30, 0, 0, false},
		{"暗图, 当前浅色", 30, 1, 0, true},
		{"亮图, 当前浅色", 220, 1, 1, false},
		{"亮图, 当前深色", 220, 0, 1, true},
		{"过渡区灰度, 当前深色", 117, 0, 0, false}, // 亮度≈0.459, 落在迟滞带内
		{"过渡区灰度, 当前浅色", 140, 1, 1, false}, // 亮度≈0.549, 落在迟滞带内
	}
	for _, c := range cases {
		path := fmt.Sprintf("%s/%s.png", dir, c.name)
		img := imaging.New(120, 120, color.NRGBA{R: c.gray, G: c.gray, B: c.gray, A: 255})
		if err := imaging.Save(img, path); err != nil {
			t.Fatalf("save: %v", err)
		}
		a, err := Analyze(path)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		mode, changed := LightDarkDecision(a.Brightness, c.current)
		if mode != c.wantMode || changed != c.wantChange {
			t.Errorf("%s: 亮度%.3f 当前%d => (%d,%v), 期望 (%d,%v)",
				c.name, a.Brightness, c.current, mode, changed, c.wantMode, c.wantChange)
		}
	}
}

// TestAnalyzeBrightnessUsesMedian 验证整图亮度用中位数:
// 暗底 + 少量亮块的图, 亮度应仍判定为暗(均值会被亮块拉高)。
func TestAnalyzeBrightnessUsesMedian(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/dark-with-spot.png"
	img := imaging.New(200, 200, color.NRGBA{R: 20, G: 20, B: 25, A: 255})
	// 右上角放一块很亮的区域(占全图 1/16)
	img = imaging.Paste(img, imaging.New(100, 100, color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Pt(100, 0))
	if err := imaging.Save(img, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	a, err := Analyze(path)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if a.Brightness > 0.2 {
		t.Errorf("整图亮度 = %.3f, 期望接近 0(中位数不受局部亮块影响)", a.Brightness)
	}
}

// TestLightDarkDecision 验证深浅色判决: 阈值、迟滞与不重复切换。
func TestLightDarkDecision(t *testing.T) {
	cases := []struct {
		name       string
		brightness float64
		current    int
		wantMode   int
		wantChange bool
	}{
		{"明显亮图, 当前浅色", 0.8, 1, 1, false},
		{"明显亮图, 当前深色", 0.8, 0, 1, true},
		{"明显暗图, 当前浅色", 0.2, 1, 0, true},
		{"明显暗图, 当前深色", 0.2, 0, 0, false},
		{"过渡区, 保持当前浅色", 0.54, 1, 1, false},
		{"过渡区, 保持当前深色", 0.46, 0, 0, false},
		{"刚好越过迟滞带下沿", 0.41, 1, 0, true},
		{"刚好越过迟滞带上沿", 0.59, 0, 1, true},
	}
	for _, c := range cases {
		mode, changed := LightDarkDecision(c.brightness, c.current)
		if mode != c.wantMode || changed != c.wantChange {
			t.Errorf("%s: 亮度%.2f 当前%d => (%d,%v), 期望 (%d,%v)",
				c.name, c.brightness, c.current, mode, changed, c.wantMode, c.wantChange)
		}
	}
}
