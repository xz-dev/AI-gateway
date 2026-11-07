// wsclient URL BODY... (WS_KEY env = API key; copied from keeper-m1 k7): send each BODY as one text frame on one socket and
// report its terminal event or a close. "{{PREV}}" in a body becomes the id
// of the last completed response; a body not starting with {"type" gets
// "type":"response.create" prepended.
// Time budget (M2 exit gate): the socket is wrapped so every write is split into 64 KiB pieces and
// every write and read completion is timestamped. Per turn a "timing" JSON line reports, in seconds
// from the turn's first frame byte: write_end, first_msg, terminal, and the longest span in which no
// byte moved in either direction (lower bound, 64 KiB resolution on the send side).
// M2 gate 7.5: WS_GAP_S = seconds to wait after a completed turn before sending the next one (default 0).
// WS_RETRY = how often a turn refused with insufficient_memory is sent again (default 0), after
// WS_RETRY_WAIT_S seconds (default 2), on a new socket (a read cut by the client's own deadline closes
// a coder/websocket conn). Every retry prints a "retry" line and the turn's outcome.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type clock struct {
	mu sync.Mutex
	ev []time.Time
}

func (c *clock) mark() { c.mu.Lock(); c.ev = append(c.ev, time.Now()); c.mu.Unlock() }

// gap returns the longest span between consecutive byte movements in [from, to].
func (c *clock) gap(from, to time.Time) (float64, float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	last, g, at := from, 0.0, 0.0
	for _, t := range c.ev {
		if t.Before(from) || t.After(to) {
			continue
		}
		if d := t.Sub(last).Seconds(); d > g {
			g, at = d, last.Sub(from).Seconds()
		}
		last = t
	}
	if d := to.Sub(last).Seconds(); d > g {
		g, at = d, last.Sub(from).Seconds()
	}
	return g, at
}

type timedConn struct {
	net.Conn
	c *clock
}

func (t *timedConn) Write(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		k := min(len(p)-n, 64<<10)
		w, err := t.Conn.Write(p[n : n+k])
		n += w
		t.c.mark()
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (t *timedConn) Read(p []byte) (int, error) {
	n, err := t.Conn.Read(p)
	if n > 0 {
		t.c.mark()
	}
	return n, err
}

var clk = &clock{}

func envFloat(name string, def float64) float64 {
	var v float64
	if _, err := fmt.Sscan(os.Getenv(name), &v); err != nil {
		return def
	}
	return v
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	gap, retries, retryWait := envFloat("WS_GAP_S", 0), int(envFloat("WS_RETRY", 0)), envFloat("WS_RETRY_WAIT_S", 2)
	d := &net.Dialer{}
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		c, err := d.DialContext(ctx, n, a)
		if err != nil {
			return nil, err
		}
		return &timedConn{Conn: c, c: clk}, nil
	}}}
	dial := func() *websocket.Conn {
		c, resp, err := websocket.Dial(ctx, os.Args[1], &websocket.DialOptions{HTTPClient: hc, HTTPHeader: map[string][]string{"Authorization": {"Bearer " + os.Getenv("WS_KEY")}}})
		if err != nil {
			st := 0
			if resp != nil {
				st = resp.StatusCode
			}
			fmt.Printf("dial-error status=%d %v\n", st, err)
			os.Exit(2)
		}
		c.SetReadLimit(-1)
		return c
	}
	c := dial()
	prev := ""
	for i, f := range os.Args[2:] {
		body, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		body = bytes.ReplaceAll(body, []byte("{{PREV}}"), []byte(prev))
		if !bytes.HasPrefix(body, []byte(`{"type"`)) {
			body = append([]byte(`{"type":"response.create",`), bytes.TrimPrefix(body, []byte("{"))...)
		}
		if i > 0 && gap > 0 {
			time.Sleep(time.Duration(gap * float64(time.Second)))
		}
		for try := 0; ; try++ {
			t0 := time.Now()
			if err := c.Write(ctx, websocket.MessageText, body); err != nil {
				fmt.Printf("turn=%d frame=%d write-error %v status=%v\n", i+1, len(body), err, websocket.CloseStatus(err))
				os.Exit(1)
			}
			writeEnd = time.Since(t0).Seconds()
			id, ok, refused := turn(ctx, c, i+1, len(body), t0)
			if ok {
				if try > 0 {
					fmt.Printf("turn=%d retry-outcome=completed retries=%d\n", i+1, try)
				}
				prev = id
				break
			}
			if !refused || try >= retries {
				if try > 0 {
					fmt.Printf("turn=%d retry-outcome=failed retries=%d\n", i+1, try)
				}
				os.Exit(1)
			}
			fmt.Printf("turn=%d retry=%d after insufficient_memory, wait %.1fs, new socket\n", i+1, try+1, retryWait)
			time.Sleep(time.Duration(retryWait * float64(time.Second)))
			c.CloseNow()
			c = dial()
		}
	}
	c.Close(websocket.StatusNormalClosure, "")
}

var writeEnd float64

func timing(i, size int, t0 time.Time, firstMsg float64, end time.Time) {
	if end.IsZero() {
		end = time.Now()
	}
	g, at := clk.gap(t0, end)
	b, _ := json.Marshal(map[string]any{"turn": i, "frame": size, "t0_unix": float64(t0.UnixNano()) / 1e9, "write_end": writeEnd, "first_msg": firstMsg,
		"terminal": end.Sub(t0).Seconds(), "longest_gap": g, "longest_gap_at": at})
	fmt.Printf("timing=%s\n", b)
}

// turn returns the response id, whether the turn completed, and whether it was refused with
// insufficient_memory (retryable).
func turn(ctx context.Context, c *websocket.Conn, i, size int, t0 time.Time) (string, bool, bool) {
	n := 0
	firstMsg := -1.0
	var termT time.Time
	defer func() { timing(i, size, t0, firstMsg, termT) }()
	for {
		_, m, err := c.Read(ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				fmt.Printf("turn=%d frame=%d frames=%d secs=%.1f BARE close code=%d reason=%q\n", i, size, n, time.Since(t0).Seconds(), ce.Code, ce.Reason)
			} else {
				fmt.Printf("turn=%d frame=%d frames=%d secs=%.1f BARE read-error %v\n", i, size, n, time.Since(t0).Seconds(), err)
			}
			return "", false, false
		}
		n++
		if firstMsg < 0 {
			firstMsg = time.Since(t0).Seconds()
		}
		var ev struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		_ = json.Unmarshal(m, &ev)
		switch ev.Type {
		case "response.completed", "error", "response.failed":
			termT = time.Now()
			h := m
			if len(h) > 300 {
				h = h[:300]
			}
			fmt.Printf("turn=%d frame=%d frames=%d secs=%.1f terminal=%s\n", i, size, n, time.Since(t0).Seconds(), h)
			if ev.Type != "response.completed" {
				// After an error event, report how the socket ends.
				rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, more, err := c.Read(rctx)
				cancel()
				fmt.Printf("turn=%d after-error next=%.120q close=%v\n", i, more, websocket.CloseStatus(err))
				return "", false, bytes.Contains(m, []byte("insufficient_memory"))
			}
			return ev.Response.ID, true, false
		}
	}
}
