package webimage

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"

	// 注册标准图片格式的解码器, 供 image.DecodeConfig 读取尺寸。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// Dimensions 返回图片的方向校正后尺寸(公开给本地目录数据源做分辨率过滤)。
// 只读取文件头, 不解码像素, 因此开销很小。
func Dimensions(path string) (int, int, error) {
	return imageDimensions(path)
}

// imageDimensions 返回图片的方向校正后尺寸。
// 只读取文件头, 不解码像素, 因此开销很小。
func imageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	w, h := cfg.Width, cfg.Height
	// EXIF 方向 5-8 表示宽高互换, 展示时需要交换
	switch exifOrientation(path) {
	case 5, 6, 7, 8:
		w, h = h, w
	}
	return w, h, nil
}

// exifOrientation 读取 JPEG 的 EXIF 方向标签(0x0112)。
// 解析失败时返回 1(正常方向)。
func exifOrientation(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	off := 2
	for off+4 <= len(data) {
		if data[off] != 0xFF {
			return 1
		}
		marker := data[off+1]
		off += 2
		// 无长度字段的标记
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		if off+2 > len(data) {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[off : off+2]))
		if size < 2 || off+size > len(data) {
			return 1
		}
		payload := data[off+2 : off+size]
		if marker == 0xE1 && len(payload) >= 8 && bytes.Equal(payload[0:6], []byte("Exif\x00\x00")) {
			if o := parseTIFFOrientation(payload[6:]); o > 0 {
				return o
			}
			return 1
		}
		off += size
	}
	return 1
}

// parseTIFFOrientation 从 TIFF 结构中取出 IFD0 的 Orientation 标签。
func parseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	pos := ifd + 2
	for i := 0; i < count; i++ {
		if pos+12 > len(tiff) {
			return 0
		}
		tag := order.Uint16(tiff[pos : pos+2])
		typ := order.Uint16(tiff[pos+2 : pos+4])
		val := order.Uint16(tiff[pos+8 : pos+10])
		if tag == 0x0112 {
			if typ == 3 || typ == 4 {
				return int(val)
			}
			return 0
		}
		pos += 12
	}
	return 0
}
