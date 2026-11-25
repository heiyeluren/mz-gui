package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"

	"golang.org/x/image/bmp"
)

const bitmapFileHeaderBytes = 14

func encodeBMP(writer io.Writer, source image.Image) error {
	return bmp.Encode(writer, source)
}

func dibToPNG(data []byte) ([]byte, error) {
	file, err := bitmapFileFromDIB(data)
	if err != nil {
		return nil, err
	}
	config, err := bmp.DecodeConfig(bytes.NewReader(file))
	if err != nil || !validImageDimensions(config.Width, config.Height) {
		return nil, ErrInvalidImage
	}
	decoded, err := bmp.Decode(bytes.NewReader(file))
	if err != nil {
		return nil, ErrInvalidImage
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return nil, ErrInvalidImage
	}
	if encoded.Len() > DefaultMaxRawImageBytes {
		return nil, ErrImageTooLarge
	}
	return encoded.Bytes(), nil
}

func bitmapFileFromDIB(data []byte) ([]byte, error) {
	if len(data) < 12 || len(data) > DefaultMaxRawImageBytes {
		return nil, ErrInvalidImage
	}
	headerSize := int(binary.LittleEndian.Uint32(data[:4]))
	if headerSize != 12 && (headerSize < 40 || headerSize > len(data)) {
		return nil, ErrInvalidImage
	}
	pixelOffset, err := dibPixelOffset(data, headerSize)
	if err != nil || pixelOffset < headerSize || pixelOffset >= len(data) {
		return nil, ErrInvalidImage
	}
	fileSize := bitmapFileHeaderBytes + len(data)
	file := make([]byte, fileSize)
	copy(file[:2], "BM")
	binary.LittleEndian.PutUint32(file[2:6], uint32(fileSize))
	binary.LittleEndian.PutUint32(file[10:14], uint32(bitmapFileHeaderBytes+pixelOffset))
	copy(file[bitmapFileHeaderBytes:], data)
	return file, nil
}

func dibPixelOffset(data []byte, headerSize int) (int, error) {
	if headerSize == 12 {
		bitCount := int(binary.LittleEndian.Uint16(data[10:12]))
		paletteEntries := 0
		if bitCount > 0 && bitCount <= 8 {
			paletteEntries = 1 << bitCount
		}
		return headerSize + paletteEntries*3, nil
	}
	if len(data) < 40 {
		return 0, errors.New("DIB 信息头不完整")
	}
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	paletteEntries := int(binary.LittleEndian.Uint32(data[32:36]))
	if paletteEntries == 0 && bitCount > 0 && bitCount <= 8 {
		paletteEntries = 1 << bitCount
	}
	extraMasks := 0
	if headerSize == 40 {
		switch compression {
		case 3:
			extraMasks = 12
		case 6:
			extraMasks = 16
		}
	}
	offset := headerSize + extraMasks + paletteEntries*4
	if offset < 0 || offset > len(data) {
		return 0, ErrInvalidImage
	}
	return offset, nil
}
