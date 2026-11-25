package streamresume

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoreReplaysFramesFromCursorAndAdvancesAck(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 8)
	cursor, err := store.Open(context.Background(), "final:main")
	if err != nil {
		t.Fatal(err)
	}
	for sequence, payload := range []string{"one", "two", "three"} {
		if _, err := store.Append(context.Background(), AppendRequest{
			StreamID: cursor.StreamID, Sequence: uint64(sequence + 1), Payload: []byte(payload),
		}); err != nil {
			t.Fatal(err)
		}
	}
	frames, advanced, err := store.Replay(context.Background(), ReplayRequest{Cursor: cursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || string(frames[0].Payload) != "one" || advanced.LastAck != 2 {
		t.Fatalf("第一次重放错误: frames=%#v cursor=%#v", frames, advanced)
	}
	frames[0].Payload[0] = 'X'
	frames, advanced, err = store.Replay(context.Background(), ReplayRequest{Cursor: advanced, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || string(frames[0].Payload) != "three" || advanced.LastAck != 3 {
		t.Fatalf("第二次重放错误: frames=%#v cursor=%#v", frames, advanced)
	}
	ack, err := store.Ack(context.Background(), advanced)
	if err != nil || ack.LastAck != 3 {
		t.Fatalf("ack 错误: cursor=%#v err=%v", ack, err)
	}
}

func TestStoreRejectsSequenceGapsAndClosedStreams(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 8)
	cursor, err := store.Open(context.Background(), "chat:stream")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), AppendRequest{StreamID: cursor.StreamID, Sequence: 2}); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("sequence gap 必须拒绝: %v", err)
	}
	if _, err := store.Append(context.Background(), AppendRequest{StreamID: cursor.StreamID, Sequence: 1, Final: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), AppendRequest{StreamID: cursor.StreamID, Sequence: 2}); !errors.Is(err, ErrStreamClosed) {
		t.Fatalf("final 后追加必须拒绝: %v", err)
	}
	if _, err := store.Ack(context.Background(), Cursor{StreamID: cursor.StreamID, ResumeToken: cursor.ResumeToken, LastAck: 9}); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("ack 超过末尾必须拒绝: %v", err)
	}
}

func TestStoreDetectsReplayWindowAndTokenFailures(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now, 2)
	cursor, err := store.Open(context.Background(), "task:progress")
	if err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= 4; sequence++ {
		if _, err := store.Append(context.Background(), AppendRequest{StreamID: cursor.StreamID, Sequence: sequence}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := store.Snapshot(cursor.StreamID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FirstSequence != 3 || snapshot.LastSequence != 4 || snapshot.FrameCount != 2 {
		t.Fatalf("重放窗口裁剪错误: %#v", snapshot)
	}
	if _, _, err := store.Replay(context.Background(), ReplayRequest{Cursor: cursor, Limit: 8}); !errors.Is(err, ErrReplayUnavailable) {
		t.Fatalf("窗口外重放必须拒绝: %v", err)
	}
	cursor.LastAck = 2
	cursor.ResumeToken = "wrong"
	if _, _, err := store.Replay(context.Background(), ReplayRequest{Cursor: cursor, Limit: 8}); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("错误 token 必须拒绝: %v", err)
	}
}

func TestStoreValidatesInputsAndContext(t *testing.T) {
	store := newTestStore(t, nil, 2)
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("非法容量必须拒绝: %v", err)
	}
	if _, err := store.Open(context.Background(), "bad stream"); !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("非法 stream id 必须拒绝: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Open(ctx, "valid"); !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("取消上下文必须拒绝: %v", err)
	}
	cursor, err := store.Open(context.Background(), "valid")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Replay(context.Background(), ReplayRequest{Cursor: cursor}); !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("非法 limit 必须拒绝: %v", err)
	}
}

func newTestStore(t *testing.T, now *time.Time, maxFrames int) *SafeStore {
	t.Helper()
	if now == nil {
		defaultNow := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
		now = &defaultNow
	}
	store, err := New(Options{
		MaxFramesPerStream: maxFrames,
		Now:                func() time.Time { return *now },
		Random:             bytes.NewReader(bytes.Repeat([]byte{7}, 4096)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
