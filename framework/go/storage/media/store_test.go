package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoreSavesVerifiedRecordingInPrivateDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "voice-record")
	store, err := NewStore(Options{
		Root:     root,
		MaxBytes: 1024,
		Now:      func() time.Time { return time.Date(2026, 8, 15, 3, 20, 0, 0, time.UTC) },
		Random:   bytes.NewReader([]byte("12345678")),
	})
	if err != nil {
		t.Fatal(err)
	}
	recording, err := store.Save(context.Background(), "audio/webm;codecs=opus", []byte{0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3}, 2500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if recording.MIMEType != "audio/webm" || recording.SizeBytes != 7 || recording.DurationMS != 2500 || filepath.Dir(recording.Path) != root {
		t.Fatalf("录音结果无效: %+v", recording)
	}
	data, err := os.ReadFile(recording.Path)
	if err != nil || !bytes.Equal(data, []byte{0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3}) {
		t.Fatalf("录音文件无效: data=%x err=%v", data, err)
	}
	if runtime.GOOS != "windows" {
		if info, statErr := os.Stat(root); statErr != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("录音目录权限无效: info=%v err=%v", info, statErr)
		}
		if info, statErr := os.Stat(recording.Path); statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("录音文件权限无效: info=%v err=%v", info, statErr)
		}
	}
}

func TestStoreUsesConfigurableBrandNeutralPrefixes(t *testing.T) {
	store, err := NewStore(Options{
		Root:   t.TempDir(),
		Now:    func() time.Time { return time.Date(2026, 11, 25, 1, 2, 3, 0, time.UTC) },
		Random: bytes.NewReader([]byte("12345678")),
	})
	if err != nil {
		t.Fatal(err)
	}
	recording, err := store.Save(context.Background(), "audio/webm", []byte{0x1a, 0x45, 0xdf, 0xa3}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(recording.Filename, "recording-") || strings.Contains(recording.Filename, "mozi") {
		t.Fatalf("默认录音文件名必须品牌中立: %s", recording.Filename)
	}
	stream, err := store.Append("aafe92e0c7ba48a2892e58bac95f506e", 1700000000000, "audio/webm", 0, []byte{0x1a, 0x45, 0xdf, 0xa3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stream.Filename, "recording-live-") {
		t.Fatalf("默认流式录音文件名必须品牌中立: %s", stream.Filename)
	}

	compatible, err := NewStore(Options{
		Root:        t.TempDir(),
		VoicePrefix: "mozi-voice",
		LivePrefix:  "mozi-live",
		Now:         func() time.Time { return time.Date(2026, 11, 25, 1, 2, 3, 0, time.UTC) },
		Random:      bytes.NewReader([]byte("12345678")),
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := compatible.Save(context.Background(), "audio/webm", []byte{0x1a, 0x45, 0xdf, 0xa3}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy.Filename, "mozi-voice-") {
		t.Fatalf("产品兼容前缀未生效: %s", legacy.Filename)
	}
}

func TestStoreRejectsUnknownFormatMagicOversizeAndSymlinkRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "voice-record")
	store, err := NewStore(Options{Root: root, MaxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		mime string
		data []byte
	}{
		{name: "unknown", mime: "audio/aac", data: []byte("abcdef")},
		{name: "magic", mime: "audio/webm", data: []byte("not-webm")},
		{name: "oversize", mime: "audio/ogg", data: []byte("OggS12345")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, saveErr := store.Save(context.Background(), test.mime, test.data, time.Second); saveErr == nil {
				t.Fatal("应拒绝无效录音")
			}
		})
	}

	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "record-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	symlinkStore, err := NewStore(Options{Root: link, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := symlinkStore.Save(context.Background(), "audio/ogg", []byte("OggS1234"), time.Second); err == nil {
		t.Fatal("应拒绝符号链接录音目录")
	}
}
