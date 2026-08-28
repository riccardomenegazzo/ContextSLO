package operator

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	if got := parseDuration("10m", time.Hour); got != 10*time.Minute {
		t.Fatalf("got %s", got)
	}
	if got := parseDuration("2s", time.Hour); got != time.Hour {
		t.Fatalf("unsafe interval %s", got)
	}
}
func TestTruncateName(t *testing.T) {
	name := truncateName("ContextSLO-" + string(make([]byte, 80)))
	if len(name) > 63 {
		t.Fatalf("name too long: %d", len(name))
	}
}
