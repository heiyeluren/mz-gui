package relaunch

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAwaitParentRejectsMalformedArguments(t *testing.T) {
	for _, args := range [][]string{{WaitFlag}, {WaitFlag, "0"}, {WaitFlag, "abc"}, {WaitFlag, strconv.Itoa(os.Getpid())}} {
		if AwaitParent(context.Background(), args) == nil {
			t.Fatalf("错误参数被接受: %v", args)
		}
	}
	if err := AwaitParent(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForRunningParentHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := waitParent(ctx, os.Getpid()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("活动进程不得被当作已经退出: %v", err)
	}
}

func TestEnvironmentOverridesArePairedAndReplaceExistingKeys(t *testing.T) {
	got := environment([]string{"HOME=/user", "ROOT=old", "cache=old"}, map[string]string{"ROOT": "new", "CACHE": "new-cache"})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "old") || !strings.Contains(joined, "ROOT=new") || !strings.Contains(joined, "CACHE=new-cache") || !strings.Contains(joined, "HOME=/user") {
		t.Fatal(joined)
	}
}
