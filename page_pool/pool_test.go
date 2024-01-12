package page_pool

import (
	"fmt"
	"github.com/go-rod/rod"
	"log"
	"sync"
	"testing"
	"time"
)

func TestPool(t *testing.T) {
	browser := rod.New().MustConnect()
	defer browser.Close()

	pc := Pool{
		InitActive:      3,           // 初始化连接数
		MaxIdle:         5,           // 最大空闲连接数
		MaxActive:       6,           // 最大连接数
		IdleTimeout:     time.Minute, // 空闲时间
		Wait:            true,        // 是否阻塞等待
		MaxConnLifetime: time.Hour,   // 连接生命周期
	}
	pool := NewPool(browser, &pc)
	defer pool.Close()

	wg := &sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go screenshot(wg, pool, "https://golang.org/")
	}
	wg.Wait()
}

func screenshot(wg *sync.WaitGroup, pool *Pool, url string) {
	defer wg.Done()
	// get page conn
	conn := pool.Get()
	defer conn.Recycle()

	// screenshot
	page := conn.Page()
	log.Printf("stats: %+v\n", pool.Stats())
	filePath := "./storages/images/" + fmt.Sprintf("feeds_%d.jpeg", time.Now().UnixNano())
	page.MustNavigate(url).MustWaitLoad().MustScreenshotFullPage(filePath)
	log.Printf("stats: %+v\n", pool.Stats())
}
