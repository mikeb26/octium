/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package httpproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mikeb26/octium/internal/am"
	"github.com/negrel/assert"
)

type HttpProxy struct {
	policyStore am.ApprovalPolicyStore
	host        string
	port        int
	transport   *http.Transport
	listener    net.Listener
	serveErr    error
}

const (
	idleConnTimeout  = 90 * time.Second
	hdrTimeout       = 15 * time.Second
	expectContinueTO = 1 * time.Second
	tlsHandshakeTO   = 10 * time.Second
	responseHdrTO    = 30 * time.Second
	proxyReadBufSize = 32 * 1024
	localhost        = "127.0.0.1"
)

func New(policyStoreIn am.ApprovalPolicyStore) *HttpProxy {
	assert.NotNil(policyStoreIn, "policyStore must not be nil")

	ret := &HttpProxy{
		policyStore: policyStoreIn,
		host:        localhost,
		port:        0,
		transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{Timeout: 30 * time.Second,
				KeepAlive: 30 * time.Second}).DialContext,
			// Allow HTTP/2 to upstream origins when possible.
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       idleConnTimeout,
			TLSHandshakeTimeout:   tlsHandshakeTO,
			ExpectContinueTimeout: expectContinueTO,
			ResponseHeaderTimeout: responseHdrTO,
		},
	}

	return ret
}

func (httpProxy *HttpProxy) ProxyAddr() string {
	return fmt.Sprintf("%v:%v", httpProxy.host, httpProxy.port)
}

func (httpProxy *HttpProxy) ListenAndServe() error {
	var err error
	httpProxy.listener, err = net.Listen("tcp", httpProxy.ProxyAddr())
	if err != nil {
		return err
	}

	if tcpAddr, ok := httpProxy.listener.Addr().(*net.TCPAddr); ok {
		httpProxy.port = tcpAddr.Port
	} else {
		return fmt.Errorf("unexpected listener address type: %T",
			httpProxy.listener.Addr())
	}

	go httpProxy.serve()

	return nil
}

// ListenAndServeTLS starts the proxy with TLS enabled.
//
// When serving TLS, the Go stdlib will negotiate HTTP/2 via ALPN if the TLS
// config advertises "h2" in NextProtos. This allows HTTP/2 clients to connect
// to the proxy.
func (httpProxy *HttpProxy) ListenAndServeTLS(certFile, keyFile string) error {
	var err error
	httpProxy.listener, err = net.Listen("tcp", httpProxy.ProxyAddr())
	if err != nil {
		return err
	}

	if tcpAddr, ok := httpProxy.listener.Addr().(*net.TCPAddr); ok {
		httpProxy.port = tcpAddr.Port
	} else {
		return fmt.Errorf("unexpected listener address type: %T",
			httpProxy.listener.Addr())
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		_ = httpProxy.listener.Close()
		return err
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	}
	httpProxy.listener = tls.NewListener(httpProxy.listener, tlsCfg)

	go httpProxy.serveWithTLSConfig(tlsCfg)
	return nil
}

func (httpProxy *HttpProxy) serve() {
	srv := &http.Server{
		Handler:           http.HandlerFunc(httpProxy.handle),
		ReadHeaderTimeout: hdrTimeout,
	}

	err := srv.Serve(httpProxy.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		httpProxy.serveErr = err
	}
	httpProxy.listener.Close()
}

func (httpProxy *HttpProxy) serveWithTLSConfig(tlsCfg *tls.Config) {
	srv := &http.Server{
		Handler:           http.HandlerFunc(httpProxy.handle),
		ReadHeaderTimeout: hdrTimeout,
		TLSConfig:         tlsCfg,
	}

	err := srv.Serve(httpProxy.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		httpProxy.serveErr = err
	}
	httpProxy.listener.Close()
}

func (httpProxy *HttpProxy) handle(w http.ResponseWriter, r *http.Request) {
	allowed, err := httpProxy.isRequestAllowedByPolicy(r)
	if err != nil {
		http.Error(w, "proxy policy error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, buildProxyPolicyForbiddenMsg(r), http.StatusForbidden)
		return
	}

	if r.Method == http.MethodConnect {
		httpProxy.handleConnect(w, r)
		return
	}
	httpProxy.handleHTTP(w, r)
}

func buildProxyPolicyForbiddenMsg(r *http.Request) string {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}

	target := "unknown"
	if host, port, scheme, ok := requestTarget(r); ok {
		target = fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port))
	} else if h := strings.TrimSpace(r.Host); h != "" {
		target = h
	}

	// IMPORTANT: this message is intended to be readable by the AI agent running
	// inside the sandbox, and should hint at the right remediation.
	return fmt.Sprintf(
		"forbidden by proxy policy (no approval for %s %s). "+
			"Network access from sandboxed commands is blocked until the user approves it. "+
			"Hint for AI agent: call the url_retrieve tool (a.k.a. retrieve_url) with approval_only=true for this URL/origin to request approval, then retry the command.",
		method, target,
	)
}

func (httpProxy *HttpProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// For an HTTP forward proxy, clients typically send absolute-form URLs.
	// If we get origin-form, reconstruct a URL from Host + RequestURI.
	outURL := *r.URL
	if outURL.Scheme == "" {
		outURL.Scheme = "http"
	}
	if outURL.Host == "" {
		outURL.Host = r.Host
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	outReq.Header = cloneHeader(r.Header)
	outReq.Host = outURL.Host

	// Hop-by-hop headers must not be forwarded.
	removeHopByHopHeaders(outReq.Header)

	resp, err := httpProxy.transport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (httpProxy *HttpProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if target == "" {
		http.Error(w, "missing host", http.StatusBadRequest)
		return
	}

	// If Host lacks a port, default to 443.
	if !strings.Contains(target, ":") {
		target += ":443"
	}

	up, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() {
		if up != nil {
			_ = up.Close()
		}
	}()

	// For HTTP/1.x CONNECT requests, net/http requires hijacking.
	// For HTTP/2, Hijacker is not supported; in that case we tunnel bytes via
	// the request/response bodies.
	hj, ok := w.(http.Hijacker)
	if !ok {
		if r.ProtoMajor >= 2 {
			httpProxy.handleConnectStream(r.Context(), w, r, up)
			up = nil
			return
		}
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	upConn := up
	up = nil

	// We now own both conns.
	// Send 200 Connection Established.
	_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")

	proxyConns(r.Context(), clientConn, upConn)
}

func (httpProxy *HttpProxy) handleConnectStream(ctx context.Context, w http.ResponseWriter, r *http.Request, up net.Conn) {
	// Best-effort: allow concurrent reads/writes for CONNECT. For HTTP/2 this is
	// already full duplex; for HTTP/1 this may fail and the caller should have
	// used hijacking.
	_ = http.NewResponseController(w).EnableFullDuplex()

	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	proxyStreamAndConn(ctx, r.Body, w, up)
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.f.Flush()
	return n, err
}

func proxyStreamAndConn(ctx context.Context, in io.Reader, out http.ResponseWriter, conn net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.CopyBuffer(conn, in, make([]byte, proxyReadBufSize))
		closeWrite(conn)
		done <- struct{}{}
	}()

	go func() {
		writer := io.Writer(out)
		if f, ok := out.(http.Flusher); ok {
			writer = &flushWriter{w: out, f: f}
		}
		_, _ = io.CopyBuffer(writer, conn, make([]byte, proxyReadBufSize))
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-done:
		<-done
	}

	_ = conn.Close()
}

func proxyConns(ctx context.Context, a, b net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.CopyBuffer(b, a, make([]byte, proxyReadBufSize))
		closeWrite(b)
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.CopyBuffer(a, b, make([]byte, proxyReadBufSize))
		closeWrite(a)
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-done:
		<-done
	}

	_ = a.Close()
	_ = b.Close()
}

type closeWriter interface {
	CloseWrite() error
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

func cloneHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vv := range h {
		cp := make([]string, len(vv))
		copy(cp, vv)
		out[k] = cp
	}
	return out
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func removeHopByHopHeaders(h http.Header) {
	// Per RFC 7230 section 6.1, the Connection header lists header fields that
	// are hop-by-hop and must not be forwarded.
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if name := strings.TrimSpace(f); name != "" {
				h.Del(name)
			}
		}
	}

	for _, k := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		h.Del(k)
	}
}
