package main

import (
	"net/http"
	"time"
)

// httpPool：全部出站 HTTP 共享的有界并发池（信号量包裹 http.Client）。
// CPA api-call、sources 抓取、ollama /api/show 都经此池，
// 任一时刻在途请求数 ≤ http_concurrency。
type httpPool struct {
	sem chan struct{}
	c   *http.Client
}

func newHTTPPool(n int, timeout time.Duration) *httpPool {
	if n <= 0 {
		n = 8
	}
	return &httpPool{
		sem: make(chan struct{}, n),
		c:   &http.Client{Timeout: timeout},
	}
}

func (p *httpPool) Do(req *http.Request) (*http.Response, error) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
		return p.c.Do(req)
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}
