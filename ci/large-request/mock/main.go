// Recording mock provider for the local capacity chain (fake credentials, no egress).
// Every request: streams the body to REC_DIR/<seq>.body while hashing it, then appends one JSON
// line to REC_DIR/requests.jsonl with method, path, headers, byte count, sha256 and forbidden-token
// hits (ticket header/member, spool markers). The mode file CTL_FILE (re-read per request) selects
// the reply: "ok" (codex/chat SSE), "413" (provider 413 JSON, byte-size cause) or "413ctx" (provider 413
// with a context-length signal). Timers: none. The server sets no read, write or idle timeout; it
// reads the whole body, then answers at once.
package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// Tokens that must never reach the provider. `\u0000lrc` is the Sub2API marker as it appears in JSON text.
var forbidden = []string{"ai_gateway_capacity_ticket", "X-AI-Gateway-Capacity-Ticket", "x-ai-gateway-capacity-ticket",
	"~lrcblob:", "aisix-spool-", `\u0000lrc`}

const Provider413Ctx = `{"error":{"message":"mock provider: your input exceeds the context window of this model","type":"invalid_request_error","code":"context_length_exceeded"}}`

const Provider413 = `{"error":{"message":"mock provider: request entity too large","type":"invalid_request_error","code":"provider_request_too_large"}}`

var seq atomic.Int64
var mu sync.Mutex

// scanner counts forbidden tokens across chunk boundaries.
type scanner struct {
	tail []byte
	hits map[string]int
}

func (s *scanner) Write(p []byte) (int, error) {
	buf := append(append([]byte{}, s.tail...), p...)
	for _, f := range forbidden {
		// count matches that end inside p (not wholly inside the old tail)
		off := 0
		for {
			i := bytes.Index(buf[off:], []byte(f))
			if i < 0 {
				break
			}
			if off+i+len(f) > len(s.tail) {
				s.hits[f]++
			}
			off += i + 1
		}
	}
	keep := 64
	if len(buf) < keep {
		keep = len(buf)
	}
	s.tail = append(s.tail[:0], buf[len(buf)-keep:]...)
	return len(p), nil
}

func main() {
	rec, ctl := os.Getenv("REC_DIR"), os.Getenv("CTL_FILE")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n := seq.Add(1)
		f, err := os.Create(fmt.Sprintf("%s/%d.body", rec, n))
		if err != nil {
			log.Fatal(err)
		}
		h := sha256.New()
		sc := &scanner{hits: map[string]int{}}
		got, rerr := io.CopyBuffer(io.MultiWriter(f, h, sc), r.Body, make([]byte, 1<<20))
		f.Close()
		hdrHits := map[string]int{}
		for k, vs := range r.Header {
			for _, v := range vs {
				for _, t := range forbidden {
					if strings.Contains(strings.ToLower(k+": "+v), strings.ToLower(t)) {
						hdrHits[t]++
					}
				}
			}
		}
		mode := "ok"
		if b, err := os.ReadFile(ctl); err == nil {
			mode = strings.TrimSpace(string(b))
		}
		hdr := map[string]string{}
		for k, vs := range r.Header {
			v := strings.Join(vs, ",")
			if k == "Authorization" || strings.Contains(strings.ToLower(k), "key") {
				v = fmt.Sprintf("<redacted len=%d>", len(v))
			}
			hdr[k] = v
		}
		line, _ := json.Marshal(map[string]any{"seq": n, "method": r.Method, "path": r.URL.Path, "bytes": got,
			"read_err": fmt.Sprint(rerr), "sha256": hex.EncodeToString(h.Sum(nil)), "body_hits": sc.hits,
			"header_hits": hdrHits, "headers": hdr, "mode": mode})
		mu.Lock()
		lf, _ := os.OpenFile(rec+"/requests.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		lf.Write(append(line, '\n'))
		lf.Close()
		mu.Unlock()
		log.Printf("%s", line)
		// delta 7.5 (bin/mock-emb, the embeddings / rerank provider behind AISIX): plain JSON bodies
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"text-embedding-v4","usage":{"prompt_tokens":%d,"total_tokens":%d}}`, got/4, got/4)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/rerank") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"rr%d","results":[{"index":0,"relevance_score":0.9}],"model":"gte-rerank-v2","usage":{"total_tokens":%d}}`, n, got/4)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-5.1-codex","object":"model","owned_by":"mock"}]}`)
			return
		}
		if mode == "413" || mode == "413ctx" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			if mode == "413ctx" {
				fmt.Fprint(w, Provider413Ctx)
			} else {
				fmt.Fprint(w, Provider413)
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(r.URL.Path, "chat/completions") {
			fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.1-codex\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok %d\"},\"finish_reason\":null}]}\n\n", got)
			fmt.Fprint(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.1-codex\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n")
			return
		}
		id := fmt.Sprintf("resp_mock%06d", n)
		resp := `{"id":"` + id + `","object":"response","created_at":1,"status":"%s","model":"gpt-5.1-codex","output":[%s],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
		msg := fmt.Sprintf(`{"id":"msg_%d","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok %d","annotations":[]}]}`, n, got)
		fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":"+resp+"}\n\n", "in_progress", "")
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"item_id\":\"msg_%d\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok %d\"}\n\n", n, got)
		fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":"+resp+"}\n\n", "completed", msg)
	})
	addr := os.Getenv("MOCK_ADDR") // listen address; default :18080
	if addr == "" {
		addr = ":18080"
	}
	// MOCK_TLS_ADDR (with MOCK_TLS_CERT / MOCK_TLS_KEY): also serve HTTPS there, HTTP/1.1 only, as the
	// provider behind Squid's TLS interception (throwaway certificate, harness only).
	if ta := os.Getenv("MOCK_TLS_ADDR"); ta != "" {
		s := &http.Server{Addr: ta, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
		go func() { log.Fatal(s.ListenAndServeTLS(os.Getenv("MOCK_TLS_CERT"), os.Getenv("MOCK_TLS_KEY"))) }()
	}
	log.Fatal(http.ListenAndServe(addr, nil))
}
