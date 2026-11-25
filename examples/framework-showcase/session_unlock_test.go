package main

import "testing"

func TestUnlockShowcaseShellSessionLocksThenUnlocks(t *testing.T) {
	if err := unlockShowcaseShellSession(); err != nil {
		t.Fatal(err)
	}
}
