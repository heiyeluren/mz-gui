package updater

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	deltaMagic          = "MZGUID1\n"
	deltaChunkCopy      = 1
	deltaChunkInsert    = 2
	maxDeltaChunks      = 4096
	maxDeltaInsertChunk = 16 << 20
)

var (
	ErrInvalidDelta      = errors.New("差分包无效")
	ErrDeltaBaseMismatch = errors.New("差分包基础摘要不匹配")
	ErrDeltaApply        = errors.New("差分包应用失败")
)

// EncodeDelta 生成可校验的受控差分包：公共前缀/后缀按 COPY，中间按 INSERT。
func EncodeDelta(base, target []byte) ([]byte, error) {
	if len(target) == 0 || int64(len(target)) > maximumArtifactBytes {
		return nil, ErrInvalidDelta
	}
	prefix := commonPrefix(base, target)
	chunks := make([]deltaChunk, 0, 3)
	if prefix > 0 {
		chunks = append(chunks, deltaChunk{kind: deltaChunkCopy, offset: 0, length: uint32(prefix)})
	}
	rest := target[prefix:]
	suffix := commonSuffix(base[prefix:], rest)
	if suffix > 0 {
		middle := rest[:len(rest)-suffix]
		if len(middle) > 0 {
			chunks = append(chunks, deltaChunk{kind: deltaChunkInsert, data: append([]byte(nil), middle...)})
		}
		chunks = append(chunks, deltaChunk{kind: deltaChunkCopy, offset: uint64(len(base) - suffix), length: uint32(suffix)})
	} else if len(rest) > 0 {
		chunks = append(chunks, deltaChunk{kind: deltaChunkInsert, data: append([]byte(nil), rest...)})
	}
	return marshalDelta(sha256.Sum256(base), sha256.Sum256(target), chunks)
}

// ApplyDelta 校验基础摘要后重建目标文件；失败时不留下半成品。
func ApplyDelta(basePath, deltaPath, destPath string) error {
	basePath, err := absRegularFile(basePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
	}
	deltaPath, err = absRegularFile(deltaPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
	}
	if destPath == "" || !filepath.IsAbs(destPath) {
		return ErrInvalidDelta
	}
	destPath = filepath.Clean(destPath)
	if _, err := os.Lstat(destPath); err == nil {
		return fmt.Errorf("%w: 目标已存在", ErrDeltaApply)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	header, chunks, err := decodeDeltaFile(deltaPath)
	if err != nil {
		return err
	}
	baseHash, err := hashFile(basePath)
	if err != nil {
		return err
	}
	if baseHash != hex.EncodeToString(header.baseSHA256[:]) {
		return ErrDeltaBaseMismatch
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	output, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(destPath)
		}
	}()
	base, err := os.Open(basePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	defer base.Close()
	baseInfo, err := base.Stat()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	written := int64(0)
	for _, chunk := range chunks {
		n, err := applyDeltaChunk(output, base, baseInfo.Size(), chunk)
		if err != nil {
			return err
		}
		written += n
		if written > maximumArtifactBytes {
			return fmt.Errorf("%w: 重建结果超过上限", ErrDeltaApply)
		}
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrDeltaApply, err)
	}
	digest, err := hashFile(destPath)
	if err != nil {
		return err
	}
	if digest != hex.EncodeToString(header.targetSHA256[:]) {
		return fmt.Errorf("%w: 重建摘要不匹配", ErrDeltaApply)
	}
	committed = true
	return nil
}

type deltaHeader struct {
	baseSHA256   [32]byte
	targetSHA256 [32]byte
}

type deltaChunk struct {
	kind   uint8
	offset uint64
	length uint32
	data   []byte
}

func marshalDelta(baseSHA, targetSHA [32]byte, chunks []deltaChunk) ([]byte, error) {
	if len(chunks) == 0 || len(chunks) > maxDeltaChunks {
		return nil, ErrInvalidDelta
	}
	buffer := bytes.NewBuffer(make([]byte, 0, 72+len(chunks)*16))
	buffer.WriteString(deltaMagic)
	buffer.Write(baseSHA[:])
	buffer.Write(targetSHA[:])
	if err := binary.Write(buffer, binary.BigEndian, uint32(len(chunks))); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDelta, err)
	}
	for _, chunk := range chunks {
		if err := writeDeltaChunk(buffer, chunk); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

func writeDeltaChunk(buffer *bytes.Buffer, chunk deltaChunk) error {
	if err := buffer.WriteByte(chunk.kind); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
	}
	switch chunk.kind {
	case deltaChunkCopy:
		if chunk.length == 0 {
			return ErrInvalidDelta
		}
		if err := binary.Write(buffer, binary.BigEndian, chunk.offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
		}
		if err := binary.Write(buffer, binary.BigEndian, chunk.length); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
		}
	case deltaChunkInsert:
		if len(chunk.data) == 0 || len(chunk.data) > maxDeltaInsertChunk {
			return ErrInvalidDelta
		}
		if err := binary.Write(buffer, binary.BigEndian, uint32(len(chunk.data))); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
		}
		if _, err := buffer.Write(chunk.data); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDelta, err)
		}
	default:
		return ErrInvalidDelta
	}
	return nil
}

func decodeDeltaFile(path string) (deltaHeader, []deltaChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return deltaHeader{}, nil, fmt.Errorf("%w: %v", ErrInvalidDelta, err)
	}
	if len(data) < 8+32+32+4 || !bytes.Equal(data[:8], []byte(deltaMagic)) {
		return deltaHeader{}, nil, ErrInvalidDelta
	}
	var header deltaHeader
	copy(header.baseSHA256[:], data[8:40])
	copy(header.targetSHA256[:], data[40:72])
	count := binary.BigEndian.Uint32(data[72:76])
	if count == 0 || count > maxDeltaChunks {
		return deltaHeader{}, nil, ErrInvalidDelta
	}
	offset := 76
	chunks := make([]deltaChunk, 0, count)
	for i := uint32(0); i < count; i++ {
		chunk, next, err := readDeltaChunk(data, offset)
		if err != nil {
			return deltaHeader{}, nil, err
		}
		chunks = append(chunks, chunk)
		offset = next
	}
	if offset != len(data) {
		return deltaHeader{}, nil, ErrInvalidDelta
	}
	return header, chunks, nil
}

func readDeltaChunk(data []byte, offset int) (deltaChunk, int, error) {
	if offset >= len(data) {
		return deltaChunk{}, 0, ErrInvalidDelta
	}
	kind := data[offset]
	offset++
	switch kind {
	case deltaChunkCopy:
		if offset+12 > len(data) {
			return deltaChunk{}, 0, ErrInvalidDelta
		}
		chunk := deltaChunk{
			kind:   kind,
			offset: binary.BigEndian.Uint64(data[offset : offset+8]),
			length: binary.BigEndian.Uint32(data[offset+8 : offset+12]),
		}
		if chunk.length == 0 {
			return deltaChunk{}, 0, ErrInvalidDelta
		}
		return chunk, offset + 12, nil
	case deltaChunkInsert:
		if offset+4 > len(data) {
			return deltaChunk{}, 0, ErrInvalidDelta
		}
		length := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if length == 0 || int(length) > maxDeltaInsertChunk || offset+int(length) > len(data) {
			return deltaChunk{}, 0, ErrInvalidDelta
		}
		return deltaChunk{kind: kind, data: data[offset : offset+int(length)]}, offset + int(length), nil
	default:
		return deltaChunk{}, 0, ErrInvalidDelta
	}
}

func applyDeltaChunk(output *os.File, base *os.File, baseSize int64, chunk deltaChunk) (int64, error) {
	switch chunk.kind {
	case deltaChunkCopy:
		end := int64(chunk.offset) + int64(chunk.length)
		if int64(chunk.offset) < 0 || end > baseSize || end < int64(chunk.offset) {
			return 0, fmt.Errorf("%w: COPY 超出基础文件", ErrDeltaApply)
		}
		if _, err := base.Seek(int64(chunk.offset), io.SeekStart); err != nil {
			return 0, fmt.Errorf("%w: %v", ErrDeltaApply, err)
		}
		written, err := io.Copy(output, io.LimitReader(base, int64(chunk.length)))
		if err != nil || written != int64(chunk.length) {
			return 0, fmt.Errorf("%w: COPY 不完整", ErrDeltaApply)
		}
		return written, nil
	case deltaChunkInsert:
		written, err := output.Write(chunk.data)
		if err != nil || written != len(chunk.data) {
			return 0, fmt.Errorf("%w: INSERT 不完整", ErrDeltaApply)
		}
		return int64(written), nil
	default:
		return 0, ErrInvalidDelta
	}
}

func commonPrefix(left, right []byte) int {
	limit := min(len(left), len(right))
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	return index
}

func commonSuffix(left, right []byte) int {
	limit := min(len(left), len(right))
	index := 0
	for index < limit && left[len(left)-1-index] == right[len(right)-1-index] {
		index++
	}
	return index
}
