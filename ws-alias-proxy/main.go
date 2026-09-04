package main

// ws-alias-proxy：CPA 前的 WebSocket 边车（managed mode）。
// 职责（穷举）：
//  1. 接受 APISIX 的 WS 升级（/v1/responses）
//  2. 别名 response.create 帧 → 托管模式：改写 model 后 HTTP POST /v1/responses
//     (stream:true) 到 CPA，SSE 事件逐条转 WS 帧；池内流前失败换目标重发，
//     流中失败透传错误事件，绝不重试
//  3. 非别名帧 → 懒拨号 CPA WS 透传（首个非别名帧到达才建立上游连接）
//  4. 目标选择：Session-Id 黏滞哈希，无则轮询；连接内粘性
//  5. 事件缺 sequence_number 时注入连接级单调递增序号
//  6. close/ping/pong 透传；帧大小上限

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen           string              `yaml:"listen"`
	Upstream         string              `yaml:"upstream"`
	Aliases          map[string][]string `yaml:"aliases"`
	StickinessHeader string              `yaml:"stickiness_header"`
	MaxFrameBytes    int64               `yaml:"max_frame_bytes"`
	DialTimeout      time.Duration       `yaml:"dial_timeout"`
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Listen:           ":8090",
		StickinessHeader: "Session-Id",
		MaxFrameBytes:    16 << 20,
		DialTimeout:      15 * time.Second,
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	if cfg.Upstream == "" {
		return nil, fmt.Errorf("upstream is required")
	}
	for alias, pool := range cfg.Aliases {
		if len(pool) == 0 {
			return nil, fmt.Errorf("aliases.%s: empty pool", alias)
		}
	}
	return cfg, nil
}

type proxy struct {
	cfg *Config
	rr  atomic.Uint64
	log *slog.Logger
}

type connState struct {
	sessionID string
	targets   map[string]int // alias → 连接内粘性起始目标
}

func (p *proxy) pickTarget(cs *connState, alias string) int {
	pool := p.cfg.Aliases[alias]
	if idx, ok := cs.targets[alias]; ok && idx < len(pool) {
		return idx
	}
	var idx int
	if cs.sessionID != "" {
		h := fnv.New32a()
		h.Write([]byte(cs.sessionID))
		h.Write([]byte{0})
		h.Write([]byte(alias))
		idx = int(h.Sum32()) % len(pool)
	} else {
		idx = int(p.rr.Add(1)-1) % len(pool)
	}
	cs.targets[alias] = idx
	return idx
}

var hopHeaders = map[string]bool{
	"connection": true, "upgrade": true, "sec-websocket-key": true,
	"sec-websocket-version": true, "sec-websocket-accept": true,
	"sec-websocket-extensions": true, "sec-websocket-protocol": true,
}

func forwardHeaders(src http.Header) http.Header {
	out := http.Header{}
	for k, vs := range src {
		if hopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	return out
}

func upstreamWSURL(base string) string {
	u := strings.TrimSuffix(base, "/") + "/v1/responses"
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	return "ws://" + u
}

// parseCreate 识别 response.create 帧并返回 model
func parseCreate(data []byte) (model string, ok bool) {
	var probe struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if json.Unmarshal(data, &probe) != nil || probe.Type != "response.create" {
		return "", false
	}
	return probe.Model, true
}

func rewriteBody(data []byte, target string) []byte {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	m["model"] = target
	m["stream"] = true
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

func eventTypeOf(data []byte) string {
	var ev struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return ""
	}
	return ev.Type
}

// isDeltaEvent：流出真实内容的判定（此前失败可安全重放）
func isDeltaEvent(typ string) bool {
	return strings.HasPrefix(typ, "response.output") || strings.Contains(typ, ".delta")
}

// clientMsg 经唯一写协程串行化到客户端
type clientMsg struct {
	mt   websocket.MessageType
	data []byte
}

// injectSeq 缺 sequence_number 时注入
func injectSeq(data []byte, seq uint64) []byte {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	if _, has := m["sequence_number"]; has {
		return data
	}
	m["sequence_number"] = seq
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

// managedCreate：别名帧的托管执行。占用自己的 HTTP 请求，可并发多条。
// 池内从 startIdx 起依次尝试；流前失败（HTTP 非 200 / 流出 delta 前的 error）换目标。
func (p *proxy) managedCreate(ctx context.Context, alias string, body []byte, startIdx int,
	hdr http.Header, out chan<- clientMsg, seq *atomic.Uint64) {

	pool := p.cfg.Aliases[alias]
	for idx := startIdx; idx < len(pool); idx++ {
		target := pool[idx]
		if p.managedAttempt(ctx, alias, target, body, hdr, out, seq) {
			return // 正常结束或流中失败（已透传），不再重试
		}
		p.log.Warn("pre-stream failure, next pool target", "alias", alias, "failed", target)
	}
	// 池耗尽：补一条终态错误事件，客户端才能感知失败
	errEv, _ := json.Marshal(map[string]any{
		"type":            "error",
		"sequence_number": seq.Add(1),
		"error":           map[string]string{"code": "pool_exhausted", "message": "all pool targets failed before streaming"},
	})
	select {
	case out <- clientMsg{websocket.MessageText, errEv}:
	case <-ctx.Done():
	}
}

// managedAttempt：单目标尝试。返回 true = 该 response 已终结（成功或流中失败已透传）。
func (p *proxy) managedAttempt(ctx context.Context, alias, target string, body []byte,
	hdr http.Header, out chan<- clientMsg, seq *atomic.Uint64) bool {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(p.cfg.Upstream, "/")+"/v1/responses", bytes.NewReader(rewriteBody(body, target)))
	if err != nil {
		return false
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.log.Warn("managed dial failed", "alias", alias, "target", target, "err", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return false
	}

	started := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), int(p.cfg.MaxFrameBytes))
	var dataLines [][]byte
	flush := func() (terminal bool, replayable bool) {
		if len(dataLines) == 0 {
			return false, false
		}
		payload := bytes.Join(dataLines, []byte("\n"))
		dataLines = dataLines[:0]
		if bytes.Equal(payload, []byte("[DONE]")) {
			return true, false
		}
		typ := eventTypeOf(payload)
		if isDeltaEvent(typ) {
			started = true
		}
		if (typ == "error" || typ == "response.failed") && !started {
			return true, true // 流前失败：吞掉，换目标
		}
		frame := injectSeq(payload, seq.Add(1))
		select {
		case out <- clientMsg{websocket.MessageText, frame}:
		case <-ctx.Done():
			return true, false
		}
		return typ == "response.completed" || typ == "response.failed" || typ == "error", false
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 { // 事件边界
			if terminal, replayable := flush(); terminal {
				return !replayable
			}
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			dataLines = append(dataLines, bytes.TrimSpace(line[5:]))
		}
		// event:/id:/retry: 行忽略，data JSON 自带 type
	}
	flush() // 流尾无空行的残留
	// 连接被上游截断：流前 → 可换目标；流中 → 已透传的内容成立，按终结处理
	return started
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		p.log.Warn("accept failed", "err", err)
		return
	}
	client.SetReadLimit(p.cfg.MaxFrameBytes)
	defer client.Close(websocket.StatusInternalError, "proxy exit")

	cs := &connState{sessionID: r.Header.Get(p.cfg.StickinessHeader), targets: map[string]int{}}
	hdr := forwardHeaders(r.Header)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out := make(chan clientMsg, 256)
	errc := make(chan error, 4)
	var seq atomic.Uint64

	// 唯一客户端写协程
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-out:
				wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
				err := client.Write(wctx, msg.mt, msg.data)
				wcancel()
				if err != nil {
					errc <- fmt.Errorf("client write: %w", err)
					return
				}
			}
		}
	}()

	// 懒拨号上游 WS（仅非别名帧需要，首个非别名帧触发）
	var upOnce sync.Once
	var upFailed atomic.Bool
	upWrite := make(chan []byte, 64)
	ensureUpstream := func() {
		upOnce.Do(func() {
			go func() {
			dctx, dcancel := context.WithTimeout(ctx, p.cfg.DialTimeout)
			up, _, derr := websocket.Dial(dctx, upstreamWSURL(p.cfg.Upstream), &websocket.DialOptions{HTTPHeader: hdr})
			dcancel()
				if derr != nil {
					p.log.Warn("passthrough upstream dial failed", "err", derr)
					upFailed.Store(true)
					errEv, _ := json.Marshal(map[string]any{
					"type":  "error",
					"error": map[string]string{"code": "upstream_unavailable", "message": "passthrough upstream dial failed"},
				})
						select {
						case out <- clientMsg{websocket.MessageText, errEv}:
						case <-ctx.Done():
						}
						return
					}
					up.SetReadLimit(p.cfg.MaxFrameBytes)
					// 上游读泵：哑转发
					go func() {
						for {
							mt, data, err := up.Read(ctx)
							if err != nil {
								errc <- fmt.Errorf("upstream read: %w", err)
								return
							}
							select {
							case out <- clientMsg{mt, data}:
							case <-ctx.Done():
								return
							}
						}
					}()
					// 上游写泵
					go func() {
						for {
							select {
							case <-ctx.Done():
								up.Close(websocket.StatusNormalClosure, "proxy exit")
								return
							case data := <-upWrite:
								if err := up.Write(ctx, websocket.MessageText, data); err != nil {
									errc <- fmt.Errorf("upstream write: %w", err)
									return
								}
							}
						}
					}()
				}()
		})
	}

	// 客户端读泵
	go func() {
		for {
			mt, data, err := client.Read(ctx)
			if err != nil {
				errc <- fmt.Errorf("client read: %w", err)
				return
			}
			if mt == websocket.MessageText {
				if model, ok := parseCreate(data); ok {
					if _, isAlias := p.cfg.Aliases[model]; isAlias {
						idx := p.pickTarget(cs, model)
						go p.managedCreate(ctx, model, data, idx, hdr, out, &seq)
						continue
					}
				}
			}
			if upFailed.Load() {
				continue // 上游不可用：丢弃非别名帧（错误事件已发）
			}
			ensureUpstream()
			select {
			case upWrite <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	err = <-errc
	cancel()
	code, reason := websocket.StatusNormalClosure, "proxy done"
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		code, reason = ce.Code, ce.Reason
	}
	client.Close(code, reason)
}

func main() {
	cfgPath := os.Getenv("WS_ALIAS_CONFIG")
	if cfgPath == "" {
		cfgPath = "/app/config.yaml"
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	p := &proxy{cfg: cfg, log: log}
	mux := http.NewServeMux()
	mux.Handle("/v1/responses", p)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	log.Info("listen", "addr", cfg.Listen, "upstream", cfg.Upstream, "aliases", len(cfg.Aliases))
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Error("server exit", "err", err)
		os.Exit(1)
	}
}
