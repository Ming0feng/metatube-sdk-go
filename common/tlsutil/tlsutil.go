// Package tlsutil provides an http.RoundTripper that impersonates browser TLS
// fingerprints (ClientHello) via the utls library, with per-host fingerprint
// rotation on 403/406 responses. It mirrors what curl_cffi's impersonate does
// for Python, allowing scrapers to bypass naive TLS-fingerprint bot detection.
//
// Requests are HTTP/1.1 only: the browser profiles advertise h2 over ALPN, but
// this RoundTripper speaks raw HTTP/1.1, so each connection's ALPN is rewritten
// to "http/1.1" after the ClientHello is built.
package tlsutil

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

// Fingerprint is a named utls ClientHello profile.
type Fingerprint struct {
	Name string
	ID   utls.ClientHelloID
}

// DefaultFingerprints is the rotation pool used when a site rejects a profile.
// Keep Chrome profiles first: they are the most common browser on the web.
var DefaultFingerprints = []Fingerprint{
	{Name: "chrome_120", ID: utls.HelloChrome_120},
	{Name: "chrome_131", ID: utls.HelloChrome_131},
	{Name: "chrome_133", ID: utls.HelloChrome_133},
	{Name: "firefox_120", ID: utls.HelloFirefox_120},
}

// Option configures a Transport.
type Option func(*Transport)

// WithProxyFunc sets the proxy resolution function (defaults to the
// environment-based http.ProxyFromEnvironment).
func WithProxyFunc(fn func(*http.Request) (*url.URL, error)) Option {
	return func(t *Transport) { t.proxyFunc = fn }
}

// WithFingerprints replaces the rotation pool.
func WithFingerprints(fps []Fingerprint) Option {
	return func(t *Transport) { t.fingerprints = fps }
}

// WithTimeout sets the per-request deadline (default 30s).
func WithTimeout(d time.Duration) Option {
	return func(t *Transport) { t.timeout = d }
}

// Transport is an http.RoundTripper using browser TLS fingerprints. It does
// not reuse connections (each request opens a fresh one), which is fine for
// low-frequency scraping.
type Transport struct {
	proxyFunc    func(*http.Request) (*url.URL, error)
	fingerprints []Fingerprint
	dialer       *net.Dialer
	timeout      time.Duration

	mu    sync.Mutex
	index map[string]int
}

// NewTransport builds a Transport with sensible defaults.
func NewTransport(opts ...Option) *Transport {
	t := &Transport{
		proxyFunc:    http.ProxyFromEnvironment,
		fingerprints: DefaultFingerprints,
		dialer:       &net.Dialer{Timeout: 15 * time.Second},
		timeout:      30 * time.Second,
		index:        make(map[string]int),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// SetProxy pins the proxy for all requests (empty string disables it).
func (t *Transport) SetProxy(proxyURL string) error {
	if proxyURL == "" {
		t.proxyFunc = nil
		return nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return err
	}
	t.proxyFunc = http.ProxyURL(u)
	return nil
}

// currentFingerprint returns the profile currently selected for a host.
func (t *Transport) currentFingerprint(host string) utls.ClientHelloID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fingerprints[t.index[host]].ID
}

// rotate advances to the next profile for host, returning false when exhausted.
func (t *Transport) rotate(host string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i := t.index[host]; i+1 < len(t.fingerprints) {
		t.index[host] = i + 1
		return true
	}
	return false
}

// RoundTrip performs the request, rotating the fingerprint when a GET is
// rejected with 403/406 (a typical TLS-fingerprint block signature).
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	for {
		resp, err := t.roundTripOnce(req)
		if err != nil {
			return nil, err
		}
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotAcceptable) &&
			req.Method == http.MethodGet && t.rotate(req.URL.Host) {
			resp.Body.Close()
			continue
		}
		return resp, nil
	}
}

func (t *Transport) roundTripOnce(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	conn, err := t.dial(ctx, req)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	clone := req.Clone(ctx)
	clone.Close = true // force Connection: close since we never reuse the conn

	// 明文 HTTP 请求直接读写，不做 TLS 握手（如 getchu 等站点仍用 http）。
	if req.URL.Scheme != "https" {
		if err := clone.Write(conn); err != nil {
			return nil, err
		}
		return http.ReadResponse(bufio.NewReader(conn), clone)
	}

	uconn := utls.UClient(conn, &utls.Config{
		ServerName:         req.URL.Hostname(),
		InsecureSkipVerify: true, //nolint:gosec // mirror curl_cffi verify=False
	}, t.currentFingerprint(req.URL.Hostname()))
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, err
	}
	// The browser profiles advertise h2; we only speak HTTP/1.1, so force it.
	for _, ext := range uconn.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		return nil, err
	}

	if err := clone.Write(uconn); err != nil {
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(uconn), clone)
}

func (t *Transport) dial(ctx context.Context, req *http.Request) (net.Conn, error) {
	addr := req.URL.Host
	if !strings.Contains(addr, ":") {
		if req.URL.Scheme == "https" {
			addr += ":443"
		} else {
			addr += ":80"
		}
	}
	if t.proxyFunc != nil {
		if proxyURL, err := t.proxyFunc(req); err != nil {
			return nil, err
		} else if proxyURL != nil {
			return t.dialProxy(ctx, proxyURL, addr)
		}
	}
	return t.dialer.DialContext(ctx, "tcp", addr)
}

// dialProxy establishes a CONNECT tunnel through proxyURL for addr and returns
// the tunneled connection, preserving any bytes the proxy already buffered.
func (t *Transport) dialProxy(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	conn, err := t.dialer.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, err
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if proxyURL.User != nil {
		if pwd, ok := proxyURL.User.Password(); ok {
			creds := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + pwd))
			connectReq.Header.Set("Proxy-Authorization", "Basic "+creds)
		}
	}
	if err := connectReq.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Do NOT close resp.Body here: for a 200 CONNECT response the body is the
	// tunnel itself, and closing it would close the connection we return.
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT to %s failed: %s", addr, resp.Status)
	}
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn replays bytes already read from the proxy tunnel before handing
// the connection over to the TLS layer.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
