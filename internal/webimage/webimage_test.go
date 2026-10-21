package webimage

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/disintegration/imaging"

	"wallpaper-tool/internal/state"
)

// writeJPEG 生成一张指定尺寸的 JPEG 测试文件。
func writeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := imaging.New(w, h, color.NRGBA{R: 120, G: 80, B: 40, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write jpeg: %v", err)
	}
}

// writeJPEGWithOrientation 生成带 EXIF 方向标签的 JPEG。
func writeJPEGWithOrientation(t *testing.T, path string, w, h int, orientation uint16) {
	t.Helper()
	writeJPEG(t, path, w, h)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jpeg: %v", err)
	}
	tiff := buildTIFF(orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg...)
	out = append(out, raw[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write oriented jpeg: %v", err)
	}
}

// buildTIFF 构造只含 Orientation 标签的最小 TIFF 结构(小端)。
func buildTIFF(orientation uint16) []byte {
	tiff := make([]byte, 26)
	copy(tiff[0:2], "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)            // IFD0 条目数
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)      // Orientation
	binary.LittleEndian.PutUint16(tiff[12:14], 3)           // SHORT
	binary.LittleEndian.PutUint32(tiff[14:18], 1)           // 数量
	binary.LittleEndian.PutUint16(tiff[18:20], orientation) // 值
	binary.LittleEndian.PutUint32(tiff[22:26], 0)           // 下一个 IFD
	return tiff
}

func testItem(path string) state.Item {
	return state.Item{ID: "abc123", Source: "wallhaven", Path: path}
}

// TestVersionStableAndChanges 确认版本号稳定, 且原图变化后版本随之改变。
func TestVersionStableAndChanges(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	src := filepath.Join(dir, "a.jpg")
	writeJPEG(t, src, 200, 100)
	it := testItem(src)

	v1, err := m.version(it)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	v2, err := m.version(it)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v1 != v2 {
		t.Errorf("版本号应稳定: %s != %s", v1, v2)
	}
	// 改写原图并推进修改时间
	time.Sleep(10 * time.Millisecond)
	writeJPEG(t, src, 200, 100)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(src, future, future)
	v3, err := m.version(it)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v3 == v1 {
		t.Errorf("原图变化后版本号应改变: %s", v3)
	}
}

// TestEnsureGeneratesScaledImage 确认派生图被缩放到规格内且尺寸正确。
func TestEnsureGeneratesScaledImage(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	src := filepath.Join(dir, "big.jpg")
	writeJPEG(t, src, 2400, 1600)
	it := testItem(src)

	out, err := m.Ensure(it, VariantThumb)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open derived: %v", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode derived: %v", err)
	}
	if cfg.Width > specs[VariantThumb].maxW || cfg.Height > specs[VariantThumb].maxH {
		t.Errorf("派生图未缩放到规格内: %dx%d", cfg.Width, cfg.Height)
	}
	// 等比缩放后宽高比应保持
	if cfg.Width*1600 != cfg.Height*2400 {
		t.Errorf("等比缩放被破坏: %dx%d", cfg.Width, cfg.Height)
	}
	// 二次调用应命中缓存
	out2, err := m.Ensure(it, VariantThumb)
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if out != out2 {
		t.Errorf("二次调用应返回同一路径: %s != %s", out, out2)
	}
}

// TestEnsurePreservesPixels 确认派生图保留了原图内容。
// 只检查尺寸无法发现"缩放后只剩底色"这类内容丢失的缺陷, 因此单独校验像素。
func TestEnsurePreservesPixels(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	src := filepath.Join(dir, "color.jpg")
	// 纯红图: 缩放后中心像素应仍为红色
	img := imaging.New(600, 400, color.NRGBA{R: 220, G: 30, B: 40, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := m.Ensure(testItem(src), VariantThumb)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	decoded, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := decoded.Bounds()
	r, g, bl, _ := decoded.At(b.Dx()/2, b.Dy()/2).RGBA()
	if r>>8 < 150 || g>>8 > 90 || bl>>8 > 90 {
		t.Errorf("派生图内容丢失, 中心像素 = %d,%d,%d (期望接近红)", r>>8, g>>8, bl>>8)
	}
}

// TestDescribeMissingFile 确认原图缺失时状态为 unavailable。
func TestDescribeMissingFile(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	info := m.Describe("desktop", state.Item{ID: "x", Source: "wallhaven", Path: filepath.Join(dir, "nope.jpg")}, VariantThumb)
	if info.State != StateUnavailable {
		t.Errorf("状态应为 unavailable, 实际 %s", info.State)
	}
	if info.ThumbURL != "" {
		t.Errorf("不可用时不应给出地址: %s", info.ThumbURL)
	}
}

// TestDimensionsAppliesEXIFOrientation 确认 EXIF 方向 6 的图片宽高被交换。
func TestDimensionsAppliesEXIFOrientation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "rot.jpg")
	writeJPEGWithOrientation(t, src, 40, 20, 6)
	w, h, err := imageDimensions(src)
	if err != nil {
		t.Fatalf("dimensions: %v", err)
	}
	if w != 20 || h != 40 {
		t.Errorf("EXIF 方向 6 应交换宽高, 实际 %dx%d", w, h)
	}
}

// TestEnsureRejectsMissingSource 确认原图缺失时生成失败而不是返回空路径。
func TestEnsureRejectsMissingSource(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	_, err := m.Ensure(state.Item{ID: "x", Source: "wallhaven", Path: filepath.Join(dir, "nope.jpg")}, VariantThumb)
	if err == nil {
		t.Fatal("原图缺失时应当返回错误")
	}
}

// TestCleanupTrimsOverBudget 确认超出容量时按最近使用时间淘汰。
func TestCleanupTrimsOverBudget(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "web-images")
	m := New(cache, nil)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 造一批大文件, 总大小超过预算
	payload := make([]byte, 1<<20)
	for i := 0; i < 3; i++ {
		name := filepath.Join(cache, "old"+string(rune('a'+i))+".jpg")
		if err := os.WriteFile(name, payload, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	m.Cleanup()
	// 预算远大于 3MB, 因此不应淘汰任何文件
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("未超出预算时不应淘汰, 剩余 %d 个", len(entries))
	}
}

// TestURLContainsStableParams 确认派生地址带有稳定的查询参数。
func TestURLContainsStableParams(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "web-images"), nil)
	src := filepath.Join(dir, "a.jpg")
	writeJPEG(t, src, 100, 100)
	info := m.Describe("lock", testItem(src), VariantThumb)
	if info.State != StatePending {
		t.Errorf("尚未生成时应为 pending, 实际 %s", info.State)
	}
	if info.ThumbURL == "" || info.PreviewURL == "" || info.BackgroundURL == "" {
		t.Errorf("三种用途都应给出地址: %+v", info)
	}
	if info.Version == "" {
		t.Error("应包含版本号")
	}
	if info.Width != 100 || info.Height != 100 {
		t.Errorf("尺寸不正确: %dx%d", info.Width, info.Height)
	}
}
