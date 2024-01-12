package page_pool

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"strings"
	"sync"
	"time"
)

var (
	nowFunc = time.Now

	// ErrPoolExhausted is returned from a pool connection method when
	// the maximum number of database connections in the pool has been reached.
	ErrPoolExhausted = errors.New("rod: page pool exhausted")

	errConnClosed = errors.New("rod: page closed")
)

// Pool Cache the browser's pages
type Pool struct {
	// InitActive number of page when the browser init.
	InitActive int
	// Maximum number of idle pages in the pool.
	MaxIdle int
	// Maximum number of pages allocated by the pool at a given time.
	// When zero, there is no limit on the number of pages in the pool.
	MaxActive int
	// Close page after remaining idle for this duration. If the value
	// is zero, then idle pages are not closed. Applications should set
	// the timeout to a value less than the server's timeout.
	IdleTimeout time.Duration
	// If Wait is true and the pool is at the MaxActive limit, then Get() waits
	// for a page to be returned to the pool before returning.
	Wait bool
	// Close pages older than this duration. If the value is zero, then
	// the pool does not close pages based on age.
	MaxConnLifetime time.Duration

	browser      *rod.Browser
	mu           sync.Mutex    // mu protects the following fields
	closed       bool          // set to true when the pool is closed.
	active       int           // the number of open pages in the pool
	initOnce     sync.Once     // the init ch once func
	ch           chan struct{} // limits open connections when p.Wait is true
	idle         idleList      // idle pages
	waitCount    int64         // total number of connections waited for.
	waitDuration time.Duration // total time waited for new connections.
}

// PoolStats contains pool statistics.
type PoolStats struct {
	// ActiveCount is the number of connections in the pool. The count includes
	// idle connections and connections in use.
	ActiveCount int

	// IdleCount is the number of idle connections in the pool.
	IdleCount int

	// WaitCount is the total number of connections waited for.
	// This value is currently not guaranteed to be 100% accurate.
	WaitCount int64

	// WaitDuration is the total time blocked waiting for a new connection.
	// This value is currently not guaranteed to be 100% accurate.
	WaitDuration time.Duration
}

func NewPool(browser *rod.Browser, poolConfig *Pool) *Pool {
	pool := &Pool{
		InitActive:      poolConfig.InitActive,
		MaxIdle:         poolConfig.MaxIdle,
		MaxActive:       poolConfig.MaxActive,
		IdleTimeout:     poolConfig.IdleTimeout,
		Wait:            poolConfig.Wait,
		MaxConnLifetime: poolConfig.MaxConnLifetime,
		browser:         browser,
		//rootCtx:     rootCtx,
		//rootCancel:  cancel,
	}

	// init the connections where the InitActive greater than 0
	for i := 0; i < poolConfig.InitActive; i++ {
		pool.active++

		conn, err := pool.newConn()
		if err != nil {
			panic(fmt.Sprintf("rod pages pool newConn err: %s", err))
		}

		if err := conn.Recycle(); err != nil {
			panic(fmt.Sprintf("rod pages pool Recycle err: %s", err))
		}
	}

	return pool
}

func (p *Pool) Get() *Conn {
	c, _ := p.GetWithCtx(context.Background())
	return c
}

func (p *Pool) GetWithCtx(ctx context.Context) (*Conn, error) {
	// Wait until there is a vacant connection in the pool.
	waited, err := p.waitVacantConn(ctx)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()

	if waited > 0 {
		p.waitCount++
		p.waitDuration += waited
	}

	// Prune stale connections at the back of the idle list.
	if p.IdleTimeout > 0 {
		n := p.idle.count
		for i := 0; i < n && p.idle.back != nil && p.idle.back.t.Add(p.IdleTimeout).Before(nowFunc()); i++ {
			pc := p.idle.back
			p.idle.popBack()
			p.mu.Unlock()
			pc.page.Close() // close the page
			p.mu.Lock()
			p.active--
		}
	}

	// Get idle connection from the front of idle list.
	for p.idle.front != nil {
		pc := p.idle.front
		p.idle.popFront()
		p.mu.Unlock()
		if p.TestOnBorrow(pc.page) == nil &&
			(p.MaxConnLifetime == 0 || nowFunc().Sub(pc.created) < p.MaxConnLifetime) {
			return &activeConn{p: p, pc: pc}, nil
		}

		pc.c.Close()
		p.mu.Lock()
		p.active--
	}

	// Check for pool closed before dialing a new connection.
	if p.closed {
		p.mu.Unlock()
		err := errors.New("squeeze: get on closed pool")
		return nil, err
	}

	// Handle limit for p.Wait == false.
	if !p.Wait && p.MaxActive > 0 && p.active >= p.MaxActive {
		p.mu.Unlock()
		return nil, ErrPoolExhausted
	}

	p.active++
	p.mu.Unlock()

	conn, err := p.newConn()
	if err != nil {
		p.mu.Lock()
		p.active--
		if p.ch != nil && !p.closed {
			p.ch <- struct{}{}
		}
		p.mu.Unlock()
		return nil, err
	}

	return conn, nil
}

// Stats returns pool's statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	stats := PoolStats{
		ActiveCount:  p.active,
		IdleCount:    p.idle.count,
		WaitCount:    p.waitCount,
		WaitDuration: p.waitDuration,
	}
	p.mu.Unlock()

	return stats
}

// ActiveCount returns the number of connections in the pool. The count
// includes idle connections and connections in use.
func (p *Pool) ActiveCount() int {
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	return active
}

// IdleCount returns the number of idle connections in the pool.
func (p *Pool) IdleCount() int {
	p.mu.Lock()
	idle := p.idle.count
	p.mu.Unlock()
	return idle
}

// Close releases the resources used by the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.active -= p.idle.count
	pc := p.idle.front
	p.idle.count = 0
	p.idle.front, p.idle.back = nil, nil
	if p.ch != nil {
		close(p.ch)
	}
	p.mu.Unlock()
	for ; pc != nil; pc = pc.next {
		pc.c.Close()
	}

	p.rootCancel()
	return nil
}

// TestOnBorrow is an function for checking the health of an idle connection
// before the connection is used again by the application. If the function
// returns an error, then the connection is closed.
func (p *Pool) TestOnBorrow(conn Conn) error {
	info, err := conn.page.Info()
	if err := chromedp.Run(c.Get(), chromedp.Tasks{chromedp.ResetViewport()}); err != nil {
		return err
	}
	return nil
}

// waitVacantConn waits for a vacant connection in pool if waiting
// is enabled and pool size is limited, otherwise returns instantly.
// If ctx expires before that, an error is returned.
//
// If there were no vacant connection in the pool right away it returns the time spent waiting
// for that connection to appear in the pool.
func (p *Pool) waitVacantConn(ctx context.Context) (waited time.Duration, err error) {
	if !p.Wait || p.MaxActive <= 0 {
		// No wait or no connection limit.
		return 0, nil
	}

	p.lazyInit()

	// wait indicates if we believe it will block so its not 100% accurate
	// however for stats it should be good enough.
	wait := len(p.ch) == 0
	var start time.Time
	if wait {
		start = time.Now()
	}

	select {
	case <-p.ch:
		// Additionally check that context hasn't expired while we were waiting,
		// because `select` picks a random `case` if several of them are "ready".
		select {
		case <-ctx.Done():
			p.ch <- struct{}{}
			return 0, ctx.Err()
		default:
		}
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	if wait {
		return time.Since(start), nil
	}
	return 0, nil
}

func (p *Pool) lazyInit() {
	p.initOnce.Do(func() {
		p.ch = make(chan struct{}, p.MaxActive)
		if p.closed {
			close(p.ch)
		} else {
			for i := 0; i < p.MaxActive; i++ {
				p.ch <- struct{}{}
			}
		}
	})
}

func (p *Pool) put(pc *poolConn, forceClose bool) error {
	p.mu.Lock()
	if !p.closed && !forceClose {
		pc.t = nowFunc()
		p.idle.pushFront(pc)
		if p.idle.count > p.MaxIdle {
			pc = p.idle.back
			p.idle.popBack()
		} else {
			pc = nil
		}
	}

	if pc != nil {
		p.mu.Unlock()
		pc.c.Close()
		p.mu.Lock()
		p.active--
	}

	if p.ch != nil && !p.closed {
		p.ch <- struct{}{}
	}
	p.mu.Unlock()
	return nil
}

func (p *Pool) newConn() (*Conn, error) {
	page, err := p.newPage()
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		pool:    p,
		page:    page,
		t:       time.Time{},
		created: time.Now(),
	}
	return conn, nil
}

func (p *Pool) newPage(url ...string) (*rod.Page, error) {
	return p.browser.Page(proto.TargetCreateTarget{URL: strings.Join(url, "/")})
}
