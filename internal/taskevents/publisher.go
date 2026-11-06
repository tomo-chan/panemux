package taskevents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"panemux/internal/tasks"
)

// Source is what the publisher observes. *Service is the production
// implementation.
type Source interface {
	// Hosts is the configured hosts besides the panemux host, read on every
	// cycle.
	Hosts() []string
	// CollectHostLive observes one host's running tasks, within its own
	// timeout.
	CollectHostLive(ctx context.Context, name string) (tasks.HostResult, []tasks.Task)
}

// Options configures a Publisher. Every field has a default.
type Options struct {
	// Interval is how long after one observation of a host finished the
	// next begins, and how often the hosts are read.
	Interval time.Duration
	// Buffer is how many frames a subscriber may fall behind before it is
	// closed.
	Buffer int
}

const (
	defaultEventInterval = 5 * time.Second
	defaultEventBuffer   = 256
)

// HostRemoved is delivered only to internal subscribers when a host leaves the
// configuration. Removal is not evidence that the host's sessions have stopped.
const HostRemoved tasks.HostStatus = "removed"

// Publisher observes hosts while browser or internal subscribers exist. Browser
// subscribers receive changes; internal subscribers receive every observation.
type Publisher struct {
	src Source
	// ctx ends only when the publisher closes: an observation in flight
	// when the last subscriber leaves runs to its own timeout.
	ctx    context.Context
	cancel context.CancelFunc

	// mu guards everything below it.
	model    *eventModel
	subs     map[*eventSubscriber]struct{}
	internal map[*internalSubscriber]struct{}
	// stop is closed when observation stops.
	stop chan struct{}
	// looping is the hosts whose observation loop is running.
	looping map[string]bool
	opts    Options
	mu      sync.Mutex
	// observing is whether observation runs.
	observing  bool
	supervised bool
	closed     bool
}

type eventSubscriber struct {
	frames chan Frame
}

type internalSubscriber struct {
	observe func(string, tasks.HostResult, []tasks.Task)
}

// SubscribeInternal shares each host observation, including unchanged tasks and
// process sets, without exposing that private metadata in browser frames. A host
// removed from configuration is delivered as HostRemoved with no tasks. The
// callback must be short and must not call back into Publisher. Callbacks are
// serialized with cancellation, so none can run after cancel returns.
func (p *Publisher) SubscribeInternal(observe func(string, tasks.HostResult, []tasks.Task)) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return func() {}
	}
	sub := &internalSubscriber{observe: observe}
	p.internal[sub] = struct{}{}
	if !p.observing {
		p.startLocked()
	}
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.internal, sub)
		p.stopIfIdleLocked()
	}
}

func (p *Publisher) stopIfIdleLocked() {
	if len(p.subs) == 0 && len(p.internal) == 0 && p.observing {
		p.observing = false
		close(p.stop)
	}
}

// New makes a publisher. Nothing is observed until a subscription is registered.
func New(src Source, opts Options) *Publisher {
	if opts.Interval <= 0 {
		opts.Interval = defaultEventInterval
	}
	if opts.Buffer <= 0 {
		opts.Buffer = defaultEventBuffer
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Publisher{
		src:      src,
		opts:     opts,
		ctx:      ctx,
		cancel:   cancel,
		model:    newEventModel(newEpoch()),
		subs:     map[*eventSubscriber]struct{}{},
		internal: map[*internalSubscriber]struct{}{},
		looping:  map[string]bool{},
	}
}

// newEpoch is 16 hex digits, chosen once per server start.
func newEpoch() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Subscribe returns the snapshot of everything currently known and the
// frames that follow it. The channel is closed by cancel, by the publisher
// closing, or when the subscriber falls Buffer frames behind. The first
// subscriber starts observation; cancel by the last one stops it.
func (p *Publisher) Subscribe() (Frame, <-chan Frame, func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sub := &eventSubscriber{frames: make(chan Frame, p.opts.Buffer)}
	if p.closed {
		close(sub.frames)
		return p.model.snapshot(), sub.frames, func() {}
	}
	if !p.observing {
		p.startLocked()
	}
	p.subs[sub] = struct{}{}
	var once sync.Once
	return p.model.snapshot(), sub.frames, func() { once.Do(func() { p.unsubscribe(sub) }) }
}

func (p *Publisher) unsubscribe(sub *eventSubscriber) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.subs[sub]; !ok {
		return
	}
	delete(p.subs, sub)
	close(sub.frames)
	p.stopIfIdleLocked()
}

// Close stops observation, cancels any observation in flight and closes
// every subscription.
func (p *Publisher) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	clear(p.internal)
	if p.observing {
		p.observing = false
		close(p.stop)
	}
	for sub := range p.subs {
		delete(p.subs, sub)
		close(sub.frames)
	}
}

// startLocked starts observation: every host becomes pending and is
// observed at once.
func (p *Publisher) startLocked() {
	p.observing = true
	p.stop = make(chan struct{})
	p.publishLocked(p.model.markPending())
	p.syncHostsLocked()
	if !p.supervised {
		p.supervised = true
		go p.superviseHosts()
	}
}

// syncHostsLocked reads the hosts, publishes the hosts added and removed
// and starts an observation loop for each host that has none.
func (p *Publisher) syncHostsLocked() {
	names := append([]string{""}, p.src.Hosts()...)
	frames := p.model.syncHosts(names)
	p.publishLocked(frames)
	for _, frame := range frames {
		if frame.Type == FrameHost && frame.Op == OpRemoved {
			name := frame.Host.Name
			p.observeInternalLocked(name, tasks.HostResult{Name: name, Status: HostRemoved}, nil)
		}
	}
	if !p.observing {
		// Publishing dropped the last subscriber: nobody is left to
		// observe a host just added for.
		return
	}
	for _, name := range names {
		if !p.looping[name] {
			p.looping[name] = true
			go p.observeHost(name)
		}
	}
}

// superviseHosts reads the hosts every Interval while observation runs.
func (p *Publisher) superviseHosts() {
	for {
		p.mu.Lock()
		if !p.observing {
			p.supervised = false
			p.mu.Unlock()
			return
		}
		stop := p.stop
		p.mu.Unlock()
		select {
		case <-time.After(p.opts.Interval):
		case <-stop:
		}
		p.mu.Lock()
		if p.observing {
			p.syncHostsLocked()
		}
		p.mu.Unlock()
	}
}

// observeHost is one host's cycle: observe, publish the difference, wait
// Interval, again. It ends when observation stops or the host is removed;
// a result that arrives after either is discarded.
func (p *Publisher) observeHost(name string) {
	for {
		result, observed := p.src.CollectHostLive(p.ctx, name)
		p.mu.Lock()
		if !p.continueLocked(name) {
			p.mu.Unlock()
			return
		}
		p.publishLocked(p.model.applyHost(name, result, observed))
		p.observeInternalLocked(name, result, observed)
		stop := p.stop
		p.mu.Unlock()

		select {
		case <-time.After(p.opts.Interval):
		case <-stop:
		}
		p.mu.Lock()
		if !p.continueLocked(name) {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
	}
}

func (p *Publisher) observeInternalLocked(name string, result tasks.HostResult, observed []tasks.Task) {
	for sub := range p.internal {
		sub.observe(name, result, observed)
	}
}

// continueLocked reports whether name's loop goes on, and marks it ended
// when it does not.
func (p *Publisher) continueLocked(name string) bool {
	_, known := p.model.hosts[name]
	if p.observing && known {
		return true
	}
	delete(p.looping, name)
	return false
}

// publishLocked hands frames to every subscriber without waiting for any:
// one whose buffer is full is closed, so it never sees a gap.
// Closing the last subscriber stops observation, as its leaving would.
func (p *Publisher) publishLocked(frames []Frame) {
	dropped := false
	for _, f := range frames {
		for sub := range p.subs {
			select {
			case sub.frames <- f:
			default:
				delete(p.subs, sub)
				close(sub.frames)
				dropped = true
			}
		}
	}
	if dropped {
		p.stopIfIdleLocked()
	}
}
