/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package httpproxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikeb26/octium/internal/am"
)

type memPolicyStore struct {
	m map[string][]am.ApprovalAction
}

func (s *memPolicyStore) Check(policyID string) ([]am.ApprovalAction, bool) {
	if s == nil || s.m == nil {
		return nil, false
	}
	a, ok := s.m[policyID]
	if !ok {
		return nil, false
	}
	cp := append([]am.ApprovalAction(nil), a...)
	return cp, true
}

func (s *memPolicyStore) Save(policyID string, actions []am.ApprovalAction) {
	if s.m == nil {
		s.m = map[string][]am.ApprovalAction{}
	}
	s.m[policyID] = append([]am.ApprovalAction(nil), actions...)
}

func TestCloneHeader_DeepCopy(t *testing.T) {
	h := http.Header{"X-Test": []string{"a", "b"}}
	cp := cloneHeader(h)
	cp.Add("X-Test", "c")
	if got := len(h["X-Test"]); got != 2 {
		t.Fatalf("expected original header slice unchanged, got len=%d", got)
	}
}

func TestRemoveHopByHopHeaders_RemovesConnectionTokens(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "X-Hop, Keep-Alive")
	h.Set("X-Hop", "1")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Upgrade", "websocket")

	removeHopByHopHeaders(h)
	if h.Get("Connection") != "" {
		t.Fatalf("expected Connection to be removed")
	}
	if h.Get("X-Hop") != "" {
		t.Fatalf("expected X-Hop to be removed due to Connection token")
	}
	if h.Get("Keep-Alive") != "" {
		t.Fatalf("expected Keep-Alive to be removed")
	}
	if h.Get("Upgrade") != "" {
		t.Fatalf("expected Upgrade to be removed")
	}
}

func TestHttpProxy_HandleHTTP_ForwardsAndStripsHopByHop(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure hop-by-hop headers are stripped.
		if r.Header.Get("Connection") != "" {
			t.Fatalf("backend received Connection header: %q", r.Header.Get("Connection"))
		}
		if r.Header.Get("X-Hop") != "" {
			t.Fatalf("backend received X-Hop header: %q", r.Header.Get("X-Hop"))
		}
		if r.Host == "" {
			t.Fatalf("expected Host to be set")
		}

		// Add hop-by-hop response headers which the proxy must strip.
		w.Header().Set("Connection", "X-Resp-Hop")
		w.Header().Set("X-Resp-Hop", "1")
		w.Header().Set("X-Keep", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "OK")
	}))
	defer backend.Close()

	store := &memPolicyStore{}
	proxy := New(store)

	// Allow reads to the backend's host:port.
	backendURL := backend.URL // http://ip:port
	req, _ := http.NewRequest(http.MethodGet, backendURL+"/p", nil)
	policyID, _, ok := buildPolicyIDForRequest(req)
	if !ok {
		t.Fatalf("expected ok building policy")
	}
	store.Save(policyID, []am.ApprovalAction{am.ApprovalActionRead})

	// Build a request as a forward proxy would receive (absolute-form).
	r, _ := http.NewRequest(http.MethodGet, backendURL+"/p", nil)
	r.Header.Set("Connection", "X-Hop")
	r.Header.Set("X-Hop", "1")

	rr := httptest.NewRecorder()
	proxy.handle(rr, r)

	resp := rr.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%q", resp.StatusCode, string(b))
	}
	if resp.Header.Get("X-Resp-Hop") != "" {
		t.Fatalf("expected X-Resp-Hop to be stripped")
	}
	if resp.Header.Get("Connection") != "" {
		t.Fatalf("expected Connection to be stripped")
	}
	if resp.Header.Get("X-Keep") != "ok" {
		t.Fatalf("expected X-Keep to be present")
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "OK" {
		t.Fatalf("unexpected response body: %q", string(b))
	}
}

func TestHttpProxy_Handle_Forbidden(t *testing.T) {
	proxy := New(&memPolicyStore{})
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	rr := httptest.NewRecorder()
	proxy.handle(rr, r)
	if rr.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Result().StatusCode)
	}
}

func TestProxyConns_CopiesAndCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	done := make(chan struct{})
	go func() {
		proxyConns(ctx, a, b)
		close(done)
	}()

	msg := []byte("hello")
	if _, err := b.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	// Data written to b should be copied to a.
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("unexpected data: %q", string(buf))
	}

	// Close one end to force completion.
	_ = b.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for proxyConns")
	}
}

func TestHttpProxy_HandleConnect_MissingHost(t *testing.T) {
	proxy := New(&memPolicyStore{})

	// Allow policy check to pass by using a request that doesn't produce a policyID.
	// We'll call handleConnect directly to focus on CONNECT behavior.
	r, _ := http.NewRequest(http.MethodConnect, "https://example.com", nil)
	r.Host = ""
	w := &hijackRecorder{rr: httptest.NewRecorder()}
	proxy.handleConnect(w, r)
	if w.rr.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.rr.Result().StatusCode)
	}
}

func TestHttpProxy_HandleConnect_HijackNotSupported(t *testing.T) {
	proxy := New(&memPolicyStore{})
	r, _ := http.NewRequest(http.MethodConnect, "https://example.com", nil)
	r.Host = "example.com:443"

	// ResponseRecorder does not implement Hijacker.
	w := httptest.NewRecorder()
	proxy.handleConnect(w, r)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

// A minimal ResponseWriter that supports hijacking.
type hijackRW struct {
	hdr http.Header
	c   net.Conn
}

func (h *hijackRW) Header() http.Header {
	if h.hdr == nil {
		h.hdr = make(http.Header)
	}
	return h.hdr
}
func (h *hijackRW) Write(p []byte) (int, error) { return len(p), nil }
func (h *hijackRW) WriteHeader(statusCode int)  {}
func (h *hijackRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h.c == nil {
		return nil, nil, errors.New("no conn")
	}
	rw := bufio.NewReadWriter(bufio.NewReader(h.c), bufio.NewWriter(h.c))
	return h.c, rw, nil
}

// hijackRecorder is a ResponseRecorder that also satisfies http.Hijacker.
// It allows testing paths in handleConnect that require Hijacker support
// without necessarily exercising the hijack itself.
type hijackRecorder struct {
	rr *httptest.ResponseRecorder
	c  net.Conn
}

func (h *hijackRecorder) Header() http.Header         { return h.rr.Header() }
func (h *hijackRecorder) Write(p []byte) (int, error) { return h.rr.Write(p) }
func (h *hijackRecorder) WriteHeader(statusCode int)  { h.rr.WriteHeader(statusCode) }
func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h.c == nil {
		return nil, nil, errors.New("no conn")
	}
	rw := bufio.NewReadWriter(bufio.NewReader(h.c), bufio.NewWriter(h.c))
	return h.c, rw, nil
}

func TestHttpProxy_HandleConnect_Writes200AndProxies(t *testing.T) {
	// Upstream TCP echo server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c) // echo
	}()

	proxy := New(&memPolicyStore{})

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	w := &hijackRW{c: server}
	r, _ := http.NewRequest(http.MethodConnect, "https://example.com", nil)
	r.Host = ln.Addr().String()

	done := make(chan struct{})
	go func() {
		proxy.handleConnect(w, r)
		close(done)
	}()

	// Read the 200 response line.
	br := bufio.NewReader(client)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(line, "200") {
		t.Fatalf("expected 200 line, got %q", line)
	}
	// Read the empty line after headers.
	_, err = br.ReadString('\n')
	if err != nil {
		t.Fatalf("read header terminator: %v", err)
	}

	// Verify tunnel works by sending bytes and expecting echo.
	payload := "ping"
	if _, err := client.Write([]byte(payload)); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("unexpected echo: %q", string(buf))
	}

	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}
