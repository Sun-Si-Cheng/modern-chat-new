package middleware

import (
	"sync"
	"testing"
)

func TestLimiterStoreBurst(t *testing.T) {
	l := NewLimiterStore(60, 3) // 1/s，突发 3
	// 前 3 次放行（突发额度）
	for i := 0; i < 3; i++ {
		if !l.Allow("k1") {
			t.Fatalf("request %d should pass", i+1)
		}
	}
	if l.Allow("k1") {
		t.Fatal("4th request should be blocked")
	}
	// 不同 key 互不影响
	if !l.Allow("k2") {
		t.Fatal("other key should pass")
	}
}

func TestLimiterStoreConcurrent(t *testing.T) {
	l := NewLimiterStore(6000, 100)
	var wg sync.WaitGroup
	passed := make(chan struct{}, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("same") {
				passed <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(passed)
	n := len(passed)
	if n != 100 {
		t.Fatalf("expect exactly 100 pass, got %d", n)
	}
}
