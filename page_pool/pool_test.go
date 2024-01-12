package page_pool

import (
	"github.com/go-rod/rod"
	"testing"
	"time"
)

func TestPool(t *testing.T) {
	browser := rod.New().MustConnect()
	defer browser.Close()

	pc := Pool{
		InitActive:      1,           // 初始化连接数
		MaxIdle:         10,          // 最大空闲连接数
		MaxActive:       20,          // 最大连接数
		IdleTimeout:     time.Minute, // 空闲时间
		Wait:            false,       // 是否阻塞等待
		MaxConnLifetime: time.Hour,   // 连接生命周期
	}
	pool := NewPool(browser, &pc)
	//defer pool.Close()

	// get a connection from browser page connection pool
	//screen(pool, "https://www.baidu.com")
	//
	//screen(pool, "https://golang.org/")
}
