package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotLoopbackURL reports a URL that is not an http or https URL of the
// host's loopback interface, which is all a relay reaches.
var ErrNotLoopbackURL = errors.New("only http and https URLs of localhost, 127.0.0.1 or [::1] can be previewed")

// ErrNoAnswer reports a loopback address on an SSH host that nothing
// answers at, such as a development server that is not running.
var ErrNoAnswer = errors.New("nothing answers on the SSH host")

// relayBindHost is the only address a relay listens on: the relayed page is
// for the browser on the panemux host, never for the network.
const relayBindHost = "127.0.0.1"

const (
	schemeHTTP   = "http"
	schemeHTTPS  = "https"
	loopbackName = "localhost"
)

// relayDialTimeout bounds connecting one browser connection to the host.
const relayDialTimeout = 10 * time.Second

// relayKey is what one relay reaches: a host's loopback address and port.
type relayKey struct {
	remote *Remote
	host   string
	port   int
}

type relay struct {
	listener net.Listener
	key      relayKey
}

// Relays republishes loopback URLs of SSH hosts on the panemux host (issue
// #387). Each host's address and port gets a listening port of its own,
// chosen free by the system on 127.0.0.1, so the same port number on two
// hosts, or on this one, never collides: each relayed page and its
// WebSockets reach the host it was opened on. A relay is kept for the
// server's lifetime and never handed to another host or port, so a tab
// opened on it outlives the pane, popup and session it came from.
type Relays struct {
	listen func(network, address string) (net.Listener, error)
	byKey  map[relayKey]*relay
	mu     sync.Mutex
	closed bool
}

// NewRelays returns a set of relays with none open.
func NewRelays() *Relays {
	return &Relays{listen: net.Listen, byKey: map[relayKey]*relay{}}
}

// ParseLoopbackURL checks that raw is an http or https URL of a loopback
// host, and returns it with the host name and port it names.
func ParseLoopbackURL(raw string) (*url.URL, string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return nil, "", 0, ErrNotLoopbackURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != schemeHTTP && scheme != schemeHTTPS {
		return nil, "", 0, ErrNotLoopbackURL
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, loopbackName) && (ip == nil || !ip.IsLoopback()) {
		return nil, "", 0, ErrNotLoopbackURL
	}
	port := 80
	if scheme == schemeHTTPS {
		port = 443
	}
	if raw := u.Port(); raw != "" {
		if port, err = strconv.Atoi(raw); err != nil || port < 1 || port > 65535 {
			return nil, "", 0, ErrNotLoopbackURL
		}
	}
	if ip == nil {
		host = loopbackName
	}
	return u, host, port, nil
}

// Open returns rawURL, a loopback URL on remote, as the URL that reaches it
// through its relay on the panemux host, opening the relay if it is not
// open yet. The path, query and fragment are kept; the host is replaced.
func (r *Relays) Open(remote *Remote, rawURL string) (string, error) {
	u, host, port, err := ParseLoopbackURL(rawURL)
	if err != nil {
		return "", err
	}
	// Reached once first, so a host or a server that does not answer is
	// reported here rather than as a blank page.
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), relayDialTimeout)
	defer cancel()
	probe, err := remote.Dial(ctx, addr)
	if err != nil {
		if errors.Is(err, ErrRemote) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrNoAnswer, err)
	}
	_ = probe.Close()
	local, err := r.ensure(relayKey{remote: remote, host: host, port: port})
	if err != nil {
		return "", err
	}
	relayed := *u
	relayed.User = nil
	relayed.Host = net.JoinHostPort(relayedHost(u, host), strconv.Itoa(local))
	if relayed.Path == "" {
		relayed.Path = "/"
	}
	return relayed.String(), nil
}

// relayedHost is the host name the relayed URL names. The relay listens on
// 127.0.0.1, which an http URL names as it is. An https URL that named
// localhost keeps that name: the relay passes TLS through untouched, so the
// browser checks the host's certificate against, and sends as SNI, the name
// in the URL, and a development certificate is issued for localhost.
func relayedHost(u *url.URL, host string) string {
	if strings.EqualFold(u.Scheme, schemeHTTPS) && host == loopbackName {
		return loopbackName
	}
	return relayBindHost
}

func (r *Relays) ensure(key relayKey) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, errors.New("the preview relays are closed")
	}
	if existing, ok := r.byKey[key]; ok {
		return relayPort(existing.listener), nil
	}
	ln, err := r.listen("tcp", net.JoinHostPort(relayBindHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("open a relay port: %w", err)
	}
	rl := &relay{listener: ln, key: key}
	r.byKey[key] = rl
	go serveRelay(rl)
	return relayPort(ln), nil
}

func relayPort(ln net.Listener) int {
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0 //coverage:exempt a TCP listener's address is a *net.TCPAddr
}

func serveRelay(rl *relay) {
	for {
		conn, err := rl.listener.Accept()
		if err != nil {
			return
		}
		go proxyRelay(rl.key, conn)
	}
}

// proxyRelay connects one browser connection to the host's loopback port
// and copies bytes both ways until either side ends.
func proxyRelay(key relayKey, local net.Conn) {
	defer func() { _ = local.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), relayDialTimeout)
	defer cancel()
	addr := net.JoinHostPort(key.host, strconv.Itoa(key.port))
	remote, err := key.remote.Dial(ctx, addr)
	if err != nil {
		log.Printf("preview relay to %s on %s: %v", addr, key.remote.Name(), err)
		return
	}
	defer func() { _ = remote.Close() }()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}

// Close closes every relay's listener. Connections already relayed end
// when their host's connection is closed (Remote.Close).
func (r *Relays) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	for _, rl := range r.byKey {
		_ = rl.listener.Close()
	}
	r.mu.Unlock()
}
