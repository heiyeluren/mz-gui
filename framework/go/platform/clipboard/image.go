package clipboard

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"

	"golang.org/x/image/draw"
)

const (
	DefaultMaxImageBytes     int64 = 6 << 20
	DefaultMaxRawImageBytes        = 64 << 20
	DefaultMaxImageDimension       = 1600
	maxImagePixels           int64 = 40_000_000
)

var (
	ErrNoImage       = errors.New("剪贴板中没有图片")
	ErrInvalidImage  = errors.New("剪贴板图片格式无效")
	ErrImageTooLarge = errors.New("剪贴板图片超过大小上限")
)

// Image 是跨平台剪贴板适配返回的规范化图片，不包含业务文件名或路径。
type Image struct {
	Data      []byte
	MIMEType  string
	Extension string
	Width     int
	Height    int
}

// ReadImage 读取系统剪贴板位图，统一缩放并编码为受限 PNG。
func ReadImage(maxBytes int64) (Image, error) {
	if maxBytes < 1 || maxBytes > DefaultMaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	data, err := platformReadImage()
	if err != nil {
		return Image{}, err
	}
	return normalizeImagePNG(data, maxBytes)
}

func normalizeImagePNG(data []byte, maxBytes int64) (Image, error) {
	if len(data) == 0 {
		return Image{}, ErrNoImage
	}
	if int64(len(data)) > DefaultMaxRawImageBytes || maxBytes < 1 || maxBytes > DefaultMaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || !validImageDimensions(config.Width, config.Height) {
		return Image{}, ErrInvalidImage
	}
	source, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" {
		return Image{}, ErrInvalidImage
	}
	width, height := scaledImageDimensions(config.Width, config.Height)
	var output image.Image = source
	if width != config.Width || height != config.Height {
		resized := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Over, nil)
		output = resized
	}
	buffer := &limitedImageBuffer{max: maxBytes}
	if err := png.Encode(buffer, output); err != nil {
		if errors.Is(err, ErrImageTooLarge) {
			return Image{}, ErrImageTooLarge
		}
		return Image{}, ErrInvalidImage
	}
	return Image{
		Data: append([]byte(nil), buffer.Bytes()...), MIMEType: "image/png", Extension: ".png", Width: width, Height: height,
	}, nil
}

func validImageDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= 32768 && height <= 32768 && int64(width)*int64(height) <= maxImagePixels
}

func scaledImageDimensions(width, height int) (int, int) {
	if width <= DefaultMaxImageDimension && height <= DefaultMaxImageDimension {
		return width, height
	}
	scale := float64(DefaultMaxImageDimension) / float64(max(width, height))
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

type limitedImageBuffer struct {
	bytes.Buffer
	max int64
}

func (buffer *limitedImageBuffer) Write(data []byte) (int, error) {
	if int64(buffer.Len())+int64(len(data)) > buffer.max {
		return 0, ErrImageTooLarge
	}
	return buffer.Buffer.Write(data)
}
