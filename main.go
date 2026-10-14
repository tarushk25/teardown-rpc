package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type app struct {
	upstream  string
	dev       bool
	max       int64
	grace     int
	remaining atomic.Int64
	client    *http.Client
	srv       *http.Server
	drainOnce sync.Once
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func (a *app) tryConsume() bool {
	for {
		cur := a.remaining.Load()
		if cur <= 0 {
			return false
		}
		if a.remaining.CompareAndSwap(cur, cur-1) {
			return true
		}
	}
}

func (a *app) drain() {
	a.drainOnce.Do(func() {
		log.Println("teardown-rpc: budget exhausted, draining")
		time.Sleep(time.Duration(a.grace) * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		a.srv.Shutdown(ctx)
		os.Exit(0)
	})
}

func (a *app) write410(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	w.Write([]byte(`{"error":"rpc lifetime exhausted"}`))
}

func (a *app) handlePost(w http.ResponseWriter, r *http.Request) {
	if !a.tryConsume() {
		a.write410(w)
		log.Printf("%s %s remaining=0", r.Method, r.URL.Path)
		go a.drain()
		return
	}
	rem := a.remaining.Load()
	log.Printf("%s %s remaining=%d", r.Method, r.URL.Path, rem)
	if rem == 0 {
		go a.drain()
	}

	if a.dev {
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var stub struct {
			ID json.RawMessage `json:"id"`
		}
		json.Unmarshal(data, &stub)
		if len(stub.ID) == 0 {
			stub.ID = json.RawMessage("1")
		}
		out := map[string]any{
			"jsonrpc": "2.0",
			"id":      stub.ID,
			"result":  "0x1bc16d674ec80000",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}

	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, a.upstream, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(upReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	rem := a.remaining.Load()
	log.Printf("%s %s remaining=%d", r.Method, r.URL.Path, rem)
	if rem <= 0 {
		a.write410(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "remaining": rem})
}

func (a *app) handleBudget(w http.ResponseWriter, r *http.Request) {
	rem := a.remaining.Load()
	log.Printf("%s %s remaining=%d", r.Method, r.URL.Path, rem)
	used := a.max - rem
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"max": a.max, "used": used, "remaining": rem})
}

func (a *app) handleRoot(w http.ResponseWriter, r *http.Request) {
	rem := a.remaining.Load()
	log.Printf("%s %s remaining=%d", r.Method, r.URL.Path, rem)
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "teardown-rpc: lifetime=%d remaining=%d\n", a.max, rem)
}

func main() {
	dev := os.Getenv("DEV") == "1"
	upstream := os.Getenv("UPSTREAM_RPC")
	if !dev && upstream == "" {
		log.Fatal("UPSTREAM_RPC is required")
	}

	port := getenv("PORT", "8545")
	maxCalls, err := strconv.Atoi(getenv("MAX_CALLS", "100"))
	if err != nil {
		log.Fatal(err)
	}
	grace, err := strconv.Atoi(getenv("GRACE_SECONDS", "5"))
	if err != nil {
		log.Fatal(err)
	}

	host := "dev-stub"
	if !dev {
		u, err := url.Parse(upstream)
		if err != nil || u.Host == "" {
			log.Fatal("invalid UPSTREAM_RPC")
		}
		host = u.Host
	}

	a := &app{
		upstream: upstream,
		dev:      dev,
		max:      int64(maxCalls),
		grace:    grace,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
	a.remaining.Store(int64(maxCalls))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", a.handlePost)
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /budget", a.handleBudget)
	mux.HandleFunc("GET /", a.handleRoot)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.srv = srv

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		os.Exit(0)
	}()

	log.Printf("teardown-rpc: listening on :%s, budget=%d, upstream=%s", port, maxCalls, host)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
