package page_pool

import (
	"github.com/go-rod/rod"
	"time"
)

type idleList struct {
	count       int
	front, back *PoolConn
}

func (l *idleList) pushFront(pc *PoolConn) {
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

type PoolConn struct {
	page       *rod.Page
	pool       *Pool
	t          time.Time // time put in the buffer pool
	created    time.Time // time created
	next, prev *PoolConn
}

// Recycle the page back to pool.
func (ac *PoolConn) Recycle() error {
	page := ac.page
	if page == nil {
		return nil
	}
	return ac.pool.put(ac, false)
}

// Page returns the rod.Page.
func (ac *PoolConn) Page() *rod.Page {
	return ac.page
}
