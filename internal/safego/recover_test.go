package safego

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRecover_LogOnly(t *testing.T) {
	// Should not crash — panic is caught and logged.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover(nil, "test", "log_only")
		panic("boom")
	}()
	<-done
}

func TestRecover_WithCallback(t *testing.T) {
	var captured string
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover(func(v any) {
			captured = v.(string)
		}, "test", "callback")
		panic("caught me")
	}()
	<-done

	if captured != "caught me" {
		t.Fatalf("expected callback to capture panic value, got: %q", captured)
	}
}

func TestRecover_IndexOutOfRange(t *testing.T) {
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover(func(v any) {
			got = fmt.Sprint(v)
		}, "test", "oob")
		var s []int
		_ = s[10] // index out of range
	}()
	<-done

	if !strings.Contains(got, "index out of range") {
		t.Fatalf("expected index-out-of-range panic, got: %q", got)
	}
}

func TestRecover_Concurrent(t *testing.T) {
	const n = 20
	var wg sync.WaitGroup
	errors := make(chan string, n)

	for range n {
		wg.Go(func() {
			defer Recover(func(v any) {
				errors <- fmt.Sprint(v)
			}, "test", "concurrent")
			panic("concurrent boom")
		})
	}
	wg.Wait()
	close(errors)

	count := 0
	for e := range errors {
		if e != "concurrent boom" {
			t.Errorf("unexpected panic value: %q", e)
		}
		count++
	}
	if count != n {
		t.Errorf("expected %d recovered panics, got %d", n, count)
	}
}

func TestRecover_NoPanic(t *testing.T) {
	called := false
	func() {
		defer Recover(func(v any) {
			called = true
		})
		// No panic — callback should not fire.
	}()
	if called {
		t.Fatal("onPanic should not be called when there is no panic")
	}
}
