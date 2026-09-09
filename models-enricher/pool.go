package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// httpPool：全部出站 HTTP 共享的有界并发池（信号量包裹 http.Client）。
// CPA api-call、sources 抓取、ollama /api/show 都经此池，
// 任一时刻在途请求数 ≤ http_concurrency。
type httpPool struct {
	sem   chan struct{}
	c     *http.Client
	cache *readCache
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
		response, err := p.c.Do(req)
		if err != nil {
			<-p.sem
			return response, err
		}
		response.Body = &pooledBody{ReadCloser: response.Body, sem: p.sem}
		return response, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

// 收到 headers 不等于请求结束；body 关闭后才释放共享并发额度。
type pooledBody struct {
	io.ReadCloser
	sem  chan struct{}
	once sync.Once
}

func (body *pooledBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(func() { <-body.sem })
	return err
}

func readBoundedBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return data, nil
}
