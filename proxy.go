package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const deadAfterFails = 5
const maxGap = 20 * time.Second
const maxCooldown = 5 * time.Minute
const exactMargin = 30 * time.Millisecond

type limitInfo struct {
	limit      int
	remaining  int
	resetAfter time.Duration
}

type bucket struct {
	limit     int
	remaining int
	resetAt   time.Time
}

func (b *bucket) refresh(now time.Time) {
	if b.limit == 0 || b.resetAt.IsZero() {
		return
	}
	if !now.Before(b.resetAt) {
		b.remaining = b.limit
		b.resetAt = time.Time{}
	}
}

func (b *bucket) allows(inflight int, probe bool) bool {
	if b.limit == 0 {
		if probe {
			return inflight < 1
		}
		return true
	}
	return b.remaining-inflight > 0
}

func (b *bucket) update(info *limitInfo, now time.Time) {
	b.limit = info.limit
	b.remaining = info.remaining
	b.resetAt = now.Add(info.resetAfter + exactMargin)
}

type proxy struct {
	client        *http.Client
	label         string
	inflight      int
	bucket        bucket
	cooldownUntil time.Time
	nextAt        time.Time
	gap           time.Duration
	fails         int
	strikes       int
	latency       float64
	streakStart   time.Time
	streakCount   int
	learned       bool
}

type poolHealth struct {
	total   int
	ready   int
	cooling int
	dead    int
	waiting int
	limited bool
	nextIn  time.Duration
}

type proxyPool struct {
	mu          sync.Mutex
	proxies     []*proxy
	direct      bool
	perProxy    int
	probe       bool
	cursor      int
	global      bucket
	globalUntil time.Time
	inflight    int
	waiting     int
	workers     int
	signal      chan struct{}
	rateLimits  atomic.Int64
	proxyErrors atomic.Int64
}

func newClient(proxyURL *url.URL, conns int) *http.Client {
	dialer := &net.Dialer{}
	dialer.Timeout = 6 * time.Second
	dialer.KeepAlive = 30 * time.Second
	transport := &http.Transport{}
	transport.DialContext = dialer.DialContext
	transport.MaxIdleConns = conns * 2
	transport.MaxIdleConnsPerHost = conns + 2
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 6 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.ExpectContinueTimeout = time.Second
	transport.ForceAttemptHTTP2 = true
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{}
	client.Transport = transport
	client.Timeout = 12 * time.Second
	return client
}

func newPool(urls []*url.URL, perProxy int, workers int, probe bool) *proxyPool {
	pool := &proxyPool{}
	pool.perProxy = perProxy
	pool.probe = probe
	pool.workers = workers
	pool.signal = make(chan struct{}, workers+1)
	if len(urls) == 0 {
		pool.direct = true
		px := &proxy{}
		px.client = newClient(nil, workers)
		px.label = "direct"
		pool.proxies = append(pool.proxies, px)
		return pool
	}
	for _, u := range urls {
		px := &proxy{}
		px.client = newClient(u, perProxy)
		px.label = u.Host
		pool.proxies = append(pool.proxies, px)
	}
	return pool
}

func (p *proxyPool) capacity() int {
	if p.direct {
		return p.workers
	}
	return p.perProxy
}

func (p *proxyPool) eligible(px *proxy, now time.Time) bool {
	if now.Before(px.cooldownUntil) {
		return false
	}
	if now.Before(px.nextAt) {
		return false
	}
	if px.inflight >= p.capacity() {
		return false
	}
	px.bucket.refresh(now)
	return px.bucket.allows(px.inflight, p.probe && !px.learned)
}

func later(a time.Time, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (p *proxyPool) nextReady(now time.Time) time.Duration {
	best := time.Duration(-1)
	consider := func(t time.Time) {
		if !t.After(now) {
			return
		}
		wait := t.Sub(now)
		if best < 0 || wait < best {
			best = wait
		}
	}
	for _, px := range p.proxies {
		ready := later(px.cooldownUntil, px.nextAt)
		if px.bucket.limit > 0 && px.bucket.remaining <= px.inflight {
			ready = later(ready, px.bucket.resetAt)
		}
		consider(ready)
	}
	globalReady := p.globalUntil
	if p.global.limit > 0 && p.global.remaining <= p.inflight {
		globalReady = later(globalReady, p.global.resetAt)
	}
	if globalReady.After(now) {
		wait := globalReady.Sub(now)
		if wait > best {
			best = wait
		}
	}
	return best
}

func (p *proxyPool) globalOpen(now time.Time) bool {
	if now.Before(p.globalUntil) {
		return false
	}
	p.global.refresh(now)
	return p.global.allows(p.inflight, false)
}

func (p *proxyPool) acquire(ctx context.Context) (*proxy, error) {
	for {
		p.mu.Lock()
		now := time.Now()
		var chosen *proxy
		if p.globalOpen(now) {
			count := len(p.proxies)
			for i := 0; i < count; i++ {
				index := (p.cursor + i) % count
				px := p.proxies[index]
				if p.eligible(px, now) {
					chosen = px
					p.cursor = index + 1
					break
				}
			}
		}
		if chosen != nil {
			chosen.inflight++
			p.inflight++
			chosen.nextAt = now.Add(chosen.gap)
			p.mu.Unlock()
			return chosen, nil
		}
		wait := p.nextReady(now)
		if wait < 0 || wait > time.Second {
			wait = time.Second
		}
		if wait < 2*time.Millisecond {
			wait = 2 * time.Millisecond
		}
		p.waiting++
		p.mu.Unlock()

		timer := time.NewTimer(wait)
		cancelled := false
		select {
		case <-ctx.Done():
			cancelled = true
		case <-p.signal:
		case <-timer.C:
		}
		timer.Stop()
		p.mu.Lock()
		p.waiting--
		p.mu.Unlock()
		if cancelled {
			return nil, ctx.Err()
		}
	}
}

func (p *proxyPool) release(px *proxy, result Result, took time.Duration, cancelled bool) {
	p.mu.Lock()
	now := time.Now()
	px.inflight--
	p.inflight--
	if !cancelled {
		p.apply(px, result, took, now)
	}
	p.mu.Unlock()
	select {
	case p.signal <- struct{}{}:
	default:
	}
}

func (p *proxyPool) apply(px *proxy, result Result, took time.Duration, now time.Time) {
	if result.Limit != nil {
		px.learned = true
		if result.Global {
			p.global.update(result.Limit, now)
		} else {
			px.bucket.update(result.Limit, now)
		}
	}
	switch result.Status {
	case StatusAvailable, StatusTaken, StatusInvalid:
		px.fails = 0
		px.strikes = 0
		px.gap = px.gap * 99 / 100
		if px.gap < 2*time.Millisecond {
			px.gap = 0
		}
		if px.streakCount == 0 {
			px.streakStart = now
		}
		px.streakCount++
		ms := float64(took.Milliseconds())
		if px.latency == 0 {
			px.latency = ms
		} else {
			px.latency = px.latency*0.8 + ms*0.2
		}
	case StatusLimited:
		p.rateLimits.Add(1)
		px.streakCount = 0
		if !result.Global && now.Before(px.cooldownUntil) {
			return
		}
		if result.Global && now.Before(p.globalUntil) {
			return
		}
		px.strikes++
		wait := result.Wait
		if result.Exact {
			wait += exactMargin
		} else {
			next := px.gap * 3 / 2
			if next < 50*time.Millisecond {
				next = 50 * time.Millisecond
			}
			if px.streakCount >= 3 {
				measured := now.Sub(px.streakStart) / time.Duration(px.streakCount)
				measured = measured * 13 / 10
				if measured > next {
					next = measured
				}
			}
			px.gap = next
			if px.gap > maxGap {
				px.gap = maxGap
			}
			shift := px.strikes - 1
			if shift > 6 {
				shift = 6
			}
			wait = wait << uint(shift)
		}
		if wait > maxCooldown {
			wait = maxCooldown
		}
		if result.Global {
			p.globalUntil = later(p.globalUntil, now.Add(wait))
		} else {
			px.cooldownUntil = later(px.cooldownUntil, now.Add(wait))
		}
	case StatusError:
		p.proxyErrors.Add(1)
		px.fails++
		if p.direct {
			px.cooldownUntil = now.Add(time.Second)
			return
		}
		shift := px.fails
		if shift > 8 {
			shift = 8
		}
		wait := (500 * time.Millisecond) << uint(shift)
		if wait > 2*time.Minute {
			wait = 2 * time.Minute
		}
		px.cooldownUntil = now.Add(wait)
	}
}

func (p *proxyPool) health() poolHealth {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	var h poolHealth
	h.total = len(p.proxies)
	h.waiting = p.waiting
	for _, px := range p.proxies {
		if px.fails >= deadAfterFails {
			h.dead++
			continue
		}
		if now.Before(px.cooldownUntil) {
			h.cooling++
			continue
		}
		h.ready++
	}
	if p.waiting >= p.workers && p.inflight == 0 {
		h.limited = true
		h.nextIn = p.nextReady(now)
	}
	if now.Before(p.globalUntil) {
		h.limited = true
		h.nextIn = p.globalUntil.Sub(now)
	}
	return h
}

type testedProxy struct {
	url     *url.URL
	latency time.Duration
}

func testProxies(urls []*url.URL, target string, progress func(done int, alive int)) []testedProxy {
	var mu sync.Mutex
	var alive []testedProxy
	var done atomic.Int64
	var aliveCount atomic.Int64
	limit := 200
	if len(urls) < limit {
		limit = len(urls)
	}
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	stopTicker := make(chan struct{})
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopTicker:
				progress(int(done.Load()), int(aliveCount.Load()))
				return
			case <-ticker.C:
				progress(int(done.Load()), int(aliveCount.Load()))
			}
		}
	}()
	for _, u := range urls {
		wg.Add(1)
		slots <- struct{}{}
		go func(proxyURL *url.URL) {
			defer wg.Done()
			defer func() {
				<-slots
			}()
			client := newClient(proxyURL, 1)
			client.Timeout = 8 * time.Second
			start := time.Now()
			req, err := http.NewRequest("GET", target, nil)
			if err != nil {
				done.Add(1)
				return
			}
			req.Header.Set("User-Agent", userAgent)
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 500 && resp.StatusCode != 407 {
					mu.Lock()
					alive = append(alive, testedProxy{url: proxyURL, latency: time.Since(start)})
					mu.Unlock()
					aliveCount.Add(1)
				}
			}
			client.CloseIdleConnections()
			done.Add(1)
		}(u)
	}
	wg.Wait()
	close(stopTicker)
	<-tickerDone
	sort.Slice(alive, func(i int, j int) bool {
		return alive[i].latency < alive[j].latency
	})
	return alive
}

func averageLatency(list []testedProxy) time.Duration {
	if len(list) == 0 {
		return 0
	}
	var total time.Duration
	for _, item := range list {
		total += item.latency
	}
	return total / time.Duration(len(list))
}
