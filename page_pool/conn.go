package page_pool

import (
	"github.com/go-rod/rod"
	"time"
)

type idleList struct {
	count       int
	front, back *Conn
}

func (l *idleList) pushFront(pc *Conn) {
	pc.next = l.front
	pc.prev = nil
	if l.count == 0 {
		l.back = pc
	} else {
		l.front.prev = pc
	}
	l.front = pc
	l.count++
}

func (l *idleList) popFront() {
	pc := l.front
	l.count--
	if l.count == 0 {
		l.front, l.back = nil, nil
	} else {
		pc.next.prev = nil
		l.front = pc.next
	}
	pc.next, pc.prev = nil, nil
}

func (l *idleList) popBack() {
	pc := l.back
	l.count--
	if l.count == 0 {
		l.front, l.back = nil, nil
	} else {
		pc.prev.next = nil
		l.back = pc.prev
	}
	pc.next, pc.prev = nil, nil
}

type Conn struct {
	//conn       *Conn
	pool       *Pool
	page       *rod.Page
	t          time.Time
	created    time.Time
	next, prev *Conn
}

// Recycle put the page back to pool.
func (ac *Conn) Recycle() error {
	pool := ac.pool
	if pool == nil {
		return nil
	}
	ac.pc = nil
	ac.pool.idle.

	return ac.p.put(pc, pc.c.Err() != nil)
}

// Err returns a non-nil value when the page is not usable.
func (ac *Conn) Err() error {
	return ac.err
}

// Page returns the rod.Page when the Err is nil.
func (ac *Conn) Page() *rod.Page {
	return ac.page
}
