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

// TestABGRFormat 验证 ABGR 打包顺序(Windows 强调色用 ABGR)。
func TestABGRFormat(t *testing.T) {
	c := color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}
	got := ABGR(c)
	want := uint32(0xFF332211)
	if got != want {
		t.Errorf("ABGR = %#08X, 期望 %#08X", got, want)
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
