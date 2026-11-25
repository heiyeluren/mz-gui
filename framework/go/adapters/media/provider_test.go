package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestLocalProviderInspectsSafeFileAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "photo.png")
	if err := os.WriteFile(inside, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := New(Options{Root: root, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close(context.Background()) })

	info, err := provider.Inspect(context.Background(), contract.MediaSource{Meta: readMeta(t, "insp-1"), URI: "file://" + inside})
	if err != nil || info.MIMEType != "image/png" || info.Bytes != 9 {
		t.Fatalf("Inspect 错误: %#v err=%v", info, err)
	}

	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Inspect(context.Background(), contract.MediaSource{Meta: readMeta(t, "insp-2"), URI: outside}); !errors.Is(err, ErrDenied) {
		t.Fatalf("越权路径错误 = %v", err)
	}
	if _, err := provider.Inspect(context.Background(), contract.MediaSource{Meta: readMeta(t, "insp-3"), URI: "https://example.com/a.png"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("远程 URI 错误 = %v", err)
	}
}

func TestLocalProviderThumbnailAndPlaybackLifecycle(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "clip.png")
	if err := os.WriteFile(inside, []byte("image-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := New(Options{Root: root, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close(context.Background()) })

	thumb, err := provider.Thumbnail(context.Background(), contract.ThumbnailRequest{
		Meta: readMeta(t, "thumb-1"), Source: contract.MediaSource{Meta: readMeta(t, "thumb-src"), URI: inside},
		Width: 64, Height: 64,
	})
	if err != nil || thumb.Bytes != 10 || thumb.SHA256 == "" || thumb.ID == "" {
		t.Fatalf("Thumbnail 错误: %#v err=%v", thumb, err)
	}

	session, err := provider.OpenPlayback(context.Background(), contract.PlaybackRequest{
		Meta: readMeta(t, "play-1"), Source: contract.MediaSource{Meta: readMeta(t, "play-src"), URI: inside},
	})
	if err != nil || session.ID == "" {
		t.Fatal(err)
	}
	paused, err := provider.Control(context.Background(), contract.PlaybackCommand{Meta: readMeta(t, "ctl-1"), SessionID: session.ID, Kind: "pause"})
	if err != nil || paused.State != "paused" {
		t.Fatalf("pause 错误: %#v err=%v", paused, err)
	}
	if err := provider.ClosePlayback(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Control(context.Background(), contract.PlaybackCommand{Meta: readMeta(t, "ctl-2"), SessionID: session.ID, Kind: "play"}); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("关闭后 Control 错误 = %v", err)
	}
}

func TestNewLocalProviderRejectsInvalidRoot(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, contract.ErrInvalidContract) {
		t.Fatalf("空 Root 错误 = %v", err)
	}
}

func readMeta(t *testing.T, requestID string) contract.RequestMeta {
	t.Helper()
	deadline, err := model.NewTimestamp(time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := model.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return contract.RequestMeta{
		RequestID: model.ID(requestID), TraceID: "trace-1", Deadline: deadline,
		Caller: model.Caller{Component: "test.media", Version: version, InstanceID: "inst-1"},
	}
}
