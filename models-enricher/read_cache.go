package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const readCacheTTL = 5 * time.Minute
const readCacheBytes int64 = 32 << 20
const readCacheEntries = 256

type readStatusError struct{ status int }

func (e readStatusError) Error() string { return fmt.Sprintf("upstream status %d", e.status) }

func staleReadAllowed(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var status readStatusError
	if errors.As(err, &status) {
		return status.status == 502 || status.status == 503 || status.status == 504
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

type readCacheEntry struct {
	stored time.Time
	used   uint64
	size   int64
}

type readFlight struct {
	done chan struct{}
	body []byte
	err  error
}

// 每次进程启动使用独立临时目录，绝不恢复旧进程的缓存；只存成功的读取结果。
// 文件预算32MiB，原子替换临时文件最多另占32MiB；生产 /tmp 使用64MiB tmpfs。
type readCache struct {
	dir     string
	mu      sync.Mutex
	entries map[string]readCacheEntry
	flights map[string]*readFlight
	bytes   int64
	clock   func() time.Time
	seq     uint64
	log     *slog.Logger
}

func newReadCache(parent string, log *slog.Logger) (*readCache, error) {
	dir, err := os.MkdirTemp(parent, "enricher-cache-")
	if err != nil {
		return nil, err
	}
	return &readCache{dir: dir, entries: map[string]readCacheEntry{}, flights: map[string]*readFlight{}, clock: time.Now, log: log}, nil
}

// URL不足以区分CPA api-call；方法、认证和其他请求头、内层请求体均参与哈希。
func readCacheKey(req *http.Request) (string, error) {
	h := sha256.New()
	if err := json.NewEncoder(h).Encode([]any{req.Method, req.URL.String(), req.Host, req.Header}); err != nil {
		return "", err
	}
	if req.Body != nil && req.Body != http.NoBody {
		if req.GetBody == nil {
			return "", errors.New("cache requires replayable request body")
		}
		body, err := req.GetBody()
		if err != nil {
			return "", err
		}
		defer body.Close()
		if _, err := io.Copy(h, body); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *readCache) path(key string) string { return filepath.Join(c.dir, key+".json") }

// 调用方持有mu，读文件与淘汰不竞争；缓存的解码/合成发生在锁外。
func (c *readCache) read(key string) ([]byte, bool) {
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	body, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	c.seq++
	entry.used = c.seq
	c.entries[key] = entry
	return body, true
}

func (c *readCache) remove(key string) error {
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	if err := os.Remove(c.path(key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	c.bytes -= entry.size
	delete(c.entries, key)
	return nil
}

func (c *readCache) store(key string, body []byte) error {
	size := int64(len(body))
	if size > readCacheBytes {
		return errors.New("response exceeds file cache budget")
	}
	for c.bytes-c.entries[key].size+size > readCacheBytes || (len(c.entries) >= readCacheEntries && c.entries[key].stored.IsZero()) {
		oldest := ""
		for candidate, entry := range c.entries {
			if candidate != key && (oldest == "" || entry.used < c.entries[oldest].used) {
				oldest = candidate
			}
		}
		if oldest == "" {
			return errors.New("file cache capacity unavailable")
		}
		if err := c.remove(oldest); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(c.dir, ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), c.path(key)); err != nil {
		return err
	}
	c.bytes += size - c.entries[key].size
	c.seq++
	c.entries[key] = readCacheEntry{stored: c.clock(), used: c.seq, size: size}
	return nil
}

func (c *readCache) load(ctx context.Context, key string, fetch func() ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && c.clock().Sub(entry.stored) < readCacheTTL {
		if body, ok := c.read(key); ok {
			c.mu.Unlock()
			return body, nil
		}
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return f.body, f.err
		}
	}
	f := &readFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	body, err := fetch()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		if storeErr := c.store(key, body); storeErr != nil {
			c.log.Warn("read cache store failed", "key", key, "err", storeErr)
		}
	} else if staleReadAllowed(err) {
		if old, ok := c.read(key); ok {
			c.log.Warn("read cache stale fallback", "key", key, "age_seconds", c.clock().Sub(c.entries[key].stored).Seconds(), "err", err)
			body, err = old, nil
		}
	} else if !errors.Is(err, context.Canceled) {
		// 权限拒绝/无效响应不借旧值掩盖，也不允许下次临时故障复活被拒绝的数据。
		if removeErr := c.remove(key); removeErr != nil {
			c.log.Warn("read cache invalidate failed", "key", key, "err", removeErr)
		}
	}
	f.body, f.err = body, err
	delete(c.flights, key)
	close(f.done)
	return body, err
}

// 仅用于已知只读请求。先检查状态、完整body和业务封装，再发布缓存。
func (p *httpPool) readJSON(req *http.Request, limit int64, cacheable bool, validate func([]byte) error) ([]byte, error) {
	fetch := func() ([]byte, error) {
		resp, err := p.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, readStatusError{resp.StatusCode}
		}
		body, err := readBoundedBody(resp.Body, limit)
		if err != nil {
			return nil, err
		}
		if !json.Valid(body) {
			return nil, errors.New("invalid upstream JSON")
		}
		if validate != nil {
			if err := validate(body); err != nil {
				return nil, err
			}
		}
		return body, nil
	}
	if !cacheable || p.cache == nil {
		return fetch()
	}
	key, err := readCacheKey(req)
	if err != nil {
		return nil, err
	}
	return p.cache.load(req.Context(), key, fetch)
}
