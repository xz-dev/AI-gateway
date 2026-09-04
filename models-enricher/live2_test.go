package main

import (
	"fmt"
	"testing"
)

func TestLiveSourcesState(t *testing.T) {
	s := newSourceCache(0)
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Printf("dev entries: %d, mpK: %d, mpS: %d\n", len(s.dev), len(s.mpK), len(s.mpS))
	h, ok := s.dev["gpt-5.6-sol"]
	fmt.Printf("dev[gpt-5.6-sol]: ok=%v ctx=%d name=%q\n", ok, h.ContextWindow, h.DisplayName)
	h2, ok2 := s.dev["grok-4.6"]
	fmt.Printf("dev[grok-4.6]: ok=%v ctx=%d\n", ok2, h2.ContextWindow)
}
