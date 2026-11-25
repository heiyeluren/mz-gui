package clock

import (
	"testing"
	"time"
)

func TestSystemClockProvidesUTCAndStoppableTicker(t *testing.T) {
	value := System{}.Now()
	if value.Location() != time.UTC || value.IsZero() {
		t.Fatalf("系统时钟必须返回 UTC: %v", value)
	}
	if _, ok := <-(System{}).After(time.Millisecond); !ok {
		t.Fatal("After channel 不应提前关闭")
	}
	ticker, err := (System{}).NewTicker(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case tick := <-ticker.C():
		if tick.Location() != time.UTC {
			t.Fatalf("Ticker 必须返回 UTC: %v", tick)
		}
	case <-time.After(time.Second):
		t.Fatal("Ticker 未触发")
	}
	ticker.Stop()
	ticker.Stop()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ticker.C():
			if !ok {
				goto stopped
			}
		case <-deadline:
			t.Fatal("Ticker 停止后 channel 未关闭")
		}
	}

stopped:
	if _, err := (System{}).NewTicker(0); err != ErrInvalidDuration {
		t.Fatalf("零间隔 ticker 应被拒绝: %v", err)
	}
}
