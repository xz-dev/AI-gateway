package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestLiveLookup(t *testing.T) {
	s := newSourceCache(time.Hour)
	_ = http.MethodGet // 直接用真实网络
	hits := s.LookupChain([]string{"modelparams_subscription", "modelparams_api_key", "models_dev"}, "gpt-5.6-sol")
	for i, h := range hits {
		fmt.Printf("hit%d: name=%q ctx=%d max=%d\n", i, h.DisplayName, h.ContextWindow, h.MaxTokens)
	}
	if len(hits) == 0 {
		t.Fatal("no hits for gpt-5.6-sol")
	}
}
