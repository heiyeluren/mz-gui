package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestNormalizeImagePNGResizesAndReturnsCanonicalMetadata(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3200, 1200))
	for y := 0; y < source.Bounds().Dy(); y++ {
		for x := 0; x < source.Bounds().Dx(); x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}

	result, err := normalizeImagePNG(encoded.Bytes(), DefaultMaxImageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if result.MIMEType != "image/png" || result.Extension != ".png" {
		t.Fatalf("图片格式元数据错误: %#v", result)
	}
	if result.Width != DefaultMaxImageDimension || result.Height != 600 {
		t.Fatalf("图片尺寸未按最长边缩放: %dx%d", result.Width, result.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("规范化结果不是有效 PNG: %v", err)
	}
	if decoded.Bounds().Dx() != result.Width || decoded.Bounds().Dy() != result.Height {
		t.Fatalf("图片数据与元数据尺寸不一致: %v", decoded.Bounds())
	}
}

func TestNormalizeImagePNGRejectsInvalidOrOversizedInput(t *testing.T) {
	if _, err := normalizeImagePNG([]byte("not-an-image"), DefaultMaxImageBytes); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("无效图片应被拒绝: %v", err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeImagePNG(encoded.Bytes(), 8); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("超过输出上限的图片应被拒绝: %v", err)
	}
}

func TestDIBToPNGSupportsCommonBitmapInfoHeader(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	source.Set(1, 1, color.RGBA{R: 220, G: 40, B: 20, A: 255})
	var bitmap bytes.Buffer
	if err := encodeBMP(&bitmap, source); err != nil {
		t.Fatal(err)
	}
	data := bitmap.Bytes()
	if len(data) <= bitmapFileHeaderBytes {
		t.Fatal("测试 BMP 数据异常")
	}

	encoded, err := dibToPNG(data[bitmapFileHeaderBytes:])
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 3 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("DIB 转 PNG 尺寸错误: %v", decoded.Bounds())
	}
}

func TestDIBToPNGSupportsBitmapV5Header(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 4, 3))
	var bitmap bytes.Buffer
	if err := encodeBMP(&bitmap, source); err != nil {
		t.Fatal(err)
	}
	dib := bitmap.Bytes()[bitmapFileHeaderBytes:]
	if len(dib) < 40 || binary.LittleEndian.Uint32(dib[:4]) != 40 {
		t.Fatal("测试 BMP 不是 BITMAPINFOHEADER")
	}
	v5 := make([]byte, len(dib)+84)
	copy(v5[:40], dib[:40])
	binary.LittleEndian.PutUint32(v5[:4], 124)
	copy(v5[124:], dib[40:])

	encoded, err := dibToPNG(v5)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 3 {
		t.Fatalf("DIBV5 转 PNG 尺寸错误: %v", decoded.Bounds())
	}
}
