package media

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestStreamDurablePrefixAndRetry(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(Options{Root: root})
	const token = "aafe92e0c7ba48a2892e58bac95f506e"
	started := time.Now().UnixMilli()
	head := []byte{0x1a, 0x45, 0xdf, 0xa3, 1}
	rec, err := store.Append(token, started, "audio/webm", 0, head)
	if err != nil {
		t.Fatal(err)
	}
	tail := []byte("audio")
	for i := 0; i < 2; i++ {
		if _, err = store.Append(token, started, "audio/webm", int64(len(head)), tail); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Append(token, started, "audio/webm", 1, tail); err == nil {
		t.Fatal("不得覆盖已写数据")
	}
	if _, err = store.Append(token, started, "audio/webm", 100, tail); err == nil {
		t.Fatal("不得跳过分片")
	}
	restarted, _ := NewStore(Options{Root: root})
	found, err := restarted.Streams()
	if err != nil || len(found) != 1 || found[0].Path != rec.Path {
		t.Fatalf("重启后恢复: %v %v", found, err)
	}
	data, err := os.ReadFile(rec.Path)
	if err != nil || !bytes.Equal(data, append(head, tail...)) {
		t.Fatalf("分片回读: %x %v", data, err)
	}
	if err = restarted.DiscardStream(token, started, "audio/webm"); err != nil {
		t.Fatal(err)
	}
}
