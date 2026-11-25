package terminal

import (
	"testing"
	"time"
)

func TestOutputRingPreservesOrderAndCopiesInput(t *testing.T) {
	ring := newOutputRing(32)
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	first := []byte("hello")
	ring.append(first, now)
	first[0] = 'X'
	ring.append([]byte("世界"), now.Add(time.Millisecond))

	snapshot := ring.snapshot(0, 32)
	if snapshot.Truncated || snapshot.NextCursor != 2 || len(snapshot.Frames) != 2 {
		t.Fatalf("输出快照错误: %#v", snapshot)
	}
	if string(snapshot.Frames[0].Data) != "hello" || string(snapshot.Frames[1].Data) != "世界" {
		t.Fatalf("输出顺序或复制隔离错误: %#v", snapshot.Frames)
	}
}

func TestOutputRingReportsExpiredCursorAfterEviction(t *testing.T) {
	ring := newOutputRing(8)
	now := time.Now().UTC()
	ring.append([]byte("1234"), now)
	ring.append([]byte("5678"), now)
	ring.append([]byte("90"), now)

	snapshot := ring.snapshot(0, 32)
	if !snapshot.Truncated || snapshot.OldestCursor != 2 || snapshot.NextCursor != 3 {
		t.Fatalf("过期 cursor 未显式截断: %#v", snapshot)
	}
	if len(snapshot.Frames) != 2 || string(snapshot.Frames[0].Data) != "5678" {
		t.Fatalf("淘汰后快照错误: %#v", snapshot.Frames)
	}
}

func TestOutputRingHonoursSnapshotLimitAndCursor(t *testing.T) {
	ring := newOutputRing(64)
	now := time.Now().UTC()
	for _, value := range []string{"aaaa", "bbbb", "cccc"} {
		ring.append([]byte(value), now)
	}

	first := ring.snapshot(0, 8)
	if len(first.Frames) != 2 || first.NextCursor != 2 || first.HasMore != true {
		t.Fatalf("分页快照错误: %#v", first)
	}
	second := ring.snapshot(first.NextCursor, 8)
	if len(second.Frames) != 1 || second.NextCursor != 3 || second.HasMore {
		t.Fatalf("续传快照错误: %#v", second)
	}
}

func TestOutputRingKeepsTailOfOversizedFrame(t *testing.T) {
	ring := newOutputRing(5)
	ring.append([]byte("123456789"), time.Now().UTC())

	snapshot := ring.snapshot(0, 10)
	if !snapshot.Truncated || len(snapshot.Frames) != 1 || string(snapshot.Frames[0].Data) != "56789" {
		t.Fatalf("超大帧尾部保留错误: %#v", snapshot)
	}
}

func TestOutputRingDoesNotClipInsideUTF8Rune(t *testing.T) {
	ring := newOutputRing(5)
	ring.append([]byte("A世界"), time.Now())
	snapshot := ring.snapshot(0, 32)
	if len(snapshot.Frames) != 1 {
		t.Fatalf("输出帧数量 = %d", len(snapshot.Frames))
	}
	if got := string(snapshot.Frames[0].Data); got != "界" {
		t.Fatalf("UTF-8 裁剪结果 = %q", got)
	}
}

func TestOutputRingKeepsPartialMarkerUntilClippedFrameIsEvicted(t *testing.T) {
	ring := newOutputRing(8)
	now := time.Now().UTC()
	ring.append([]byte("123456789"), now)
	ring.append([]byte("x"), now)
	if snapshot := ring.snapshot(0, 16); !snapshot.Truncated {
		t.Fatalf("超大帧仍在缓冲区时必须保留截断标记: %#v", snapshot)
	}

	ring.append([]byte("ABCDEFGH"), now)
	if snapshot := ring.snapshot(2, 16); snapshot.Truncated || len(snapshot.Frames) != 1 {
		t.Fatalf("超大帧淘汰后不应继续报告截断: %#v", snapshot)
	}
}

func TestOutputRingBoundsHighFrequencyOverflow(t *testing.T) {
	const maxBytes = 4 << 20
	ring := newOutputRing(maxBytes)
	now := time.Now().UTC()
	chunk := make([]byte, 32<<10)
	for index := range chunk {
		chunk[index] = 'A'
	}

	produced := 0
	for produced < 12<<20 {
		ring.append(chunk, now)
		produced += len(chunk)
		now = now.Add(time.Microsecond)
	}

	used := ringUsed(ring)
	if used > maxBytes || used == 0 {
		t.Fatalf("高频追加后缓冲区应有上界: used=%d max=%d", used, maxBytes)
	}

	snapshot := ring.snapshot(0, 1<<30)
	if !snapshot.Truncated || snapshot.OldestCursor == 0 || snapshot.NextCursor < snapshot.OldestCursor {
		t.Fatalf("超量淘汰后快照应可读且报告截断: %#v", snapshot)
	}
	size := 0
	var previous uint64
	for _, frame := range snapshot.Frames {
		if frame.Sequence <= previous {
			t.Fatalf("快照游标必须单调递增: prev=%d seq=%d", previous, frame.Sequence)
		}
		previous = frame.Sequence
		size += len(frame.Data)
	}
	if size > maxBytes || size != used {
		t.Fatalf("快照字节数越界或与 used 不一致: size=%d used=%d", size, used)
	}
}

func TestOutputRingCursorStaysMonotonicWhileEvicting(t *testing.T) {
	const maxBytes = 4 << 20
	ring := newOutputRing(maxBytes)
	now := time.Now().UTC()
	chunk := make([]byte, 8<<10)
	for index := range chunk {
		chunk[index] = 'B'
	}

	var cursor, last uint64
	for round := 0; round < 3; round++ {
		for produced := 0; produced < 6<<20; produced += len(chunk) {
			ring.append(chunk, now)
			now = now.Add(time.Nanosecond)
			snapshot := ring.snapshot(cursor, 64<<10)
			if snapshot.NextCursor < last {
				t.Fatalf("续传游标回退: last=%d next=%d truncated=%v", last, snapshot.NextCursor, snapshot.Truncated)
			}
			last = snapshot.NextCursor
			cursor = snapshot.NextCursor
		}
	}
	lagging := ring.snapshot(0, 64<<10)
	if !lagging.Truncated || lagging.OldestCursor == 0 || lagging.NextCursor < lagging.OldestCursor {
		t.Fatalf("过期 cursor 必须仍可读并报告截断: %#v", lagging)
	}
	if ringUsed(ring) > maxBytes {
		t.Fatalf("淘汰过程中缓冲区越界: %d", ringUsed(ring))
	}
}

func TestOutputRingBoundsTinyFrameMetadataAndKeepsLatestCursor(t *testing.T) {
	const frames = maxOutputFrames
	ring := newOutputRing(4 << 20)
	now := time.Now()
	for index := 0; index < frames*3; index++ {
		ring.append([]byte{'x'}, now)
	}
	if len(ring.frames) != frames || ringUsed(ring) != frames {
		t.Fatalf("微小输出帧元数据没有上界: frames=%d payload=%d", len(ring.frames), ringUsed(ring))
	}
	first := ring.snapshot(0, 256)
	if !first.Truncated || !first.HasMore || first.OldestCursor != frames*2+1 || first.NextCursor != frames*2+256 {
		t.Fatalf("帧上限淘汰没有沿用游标/截断合同: %#v", first)
	}
	for first.HasMore {
		first = ring.snapshot(first.NextCursor, 256)
	}
	if first.NextCursor != frames*3 {
		t.Fatalf("最新输出游标丢失: %d", first.NextCursor)
	}
	ring.append([]byte("最新结果"), now)
	last := ring.snapshot(first.NextCursor, 256)
	if last.Truncated || last.HasMore || len(last.Frames) != 1 || string(last.Frames[0].Data) != "最新结果" || last.NextCursor != frames*3+1 {
		t.Fatalf("已发cursor之后的最新数据被裁掉: %#v", last)
	}
}

func ringUsed(ring *outputRing) int {
	ring.mu.RLock()
	defer ring.mu.RUnlock()
	return ring.usedBytes
}

func TestOutputRingRetainsSmallTUIFramesWithinPayloadBudget(t *testing.T) {
	ring := newOutputRing(4 << 20)
	data := make([]byte, 128)
	for i := 0; i < 16384; i++ {
		ring.append(data, time.Now())
	}
	snapshot := ring.snapshot(0, 4<<20)
	if snapshot.Truncated || snapshot.OldestCursor != 1 || len(snapshot.Frames) != maxSnapshotFrames || !snapshot.HasMore {
		t.Fatalf("仅2MiB小帧不应提前截断: oldest=%d frames=%d", snapshot.OldestCursor, len(snapshot.Frames))
	}
	total := len(snapshot.Frames)
	for snapshot.HasMore {
		snapshot = ring.snapshot(snapshot.NextCursor, 4<<20)
		total += len(snapshot.Frames)
	}
	if total != 16384 || snapshot.NextCursor != 16384 {
		t.Fatal("分页后内容或游标丢失")
	}
}
