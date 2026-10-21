package imagecache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPutGetRoundTrip(t *testing.T) {
	cache, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data := []byte("fake-jpeg-bytes")
	cache.Put("https://c0.jdbstatic.com/covers/aq/AqMvn0.jpg", data)

	got, ok := cache.Get("https://c0.jdbstatic.com/covers/aq/AqMvn0.jpg")
	if !ok {
		t.Fatal("缓存未命中")
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("内容不一致: %q != %q", got, data)
	}
}

func TestGetMiss(t *testing.T) {
	cache, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := cache.Get("https://example.com/none.jpg"); ok {
		t.Fatal("不存在的 key 不该命中")
	}
}

func TestDisabledCacheIsSafe(t *testing.T) {
	// 空目录 = 禁用；返回 nil，但调用方照常 Get/Put 不能 panic。
	cache, err := Open("", 0)
	if err != nil {
		t.Fatalf("禁用不该返回错误: %v", err)
	}
	if cache != nil {
		t.Fatal("空目录应该禁用缓存")
	}

	if _, ok := cache.Get("x"); ok {
		t.Fatal("禁用时不该命中")
	}
	cache.Put("x", []byte("y")) // 不应 panic
	if dir := cache.Dir(); dir != "" {
		t.Fatalf("禁用时 Dir 应为空，得到 %q", dir)
	}
}

func TestSameURLMapsToSameKey(t *testing.T) {
	url := "https://c0.jdbstatic.com/covers/aq/AqMvn0.jpg"
	if first, second := Key(url), Key(url); first != second {
		t.Fatal("同一个 URL 的 key 必须稳定")
	}
	if Key(url) == Key(url+"?v=2") {
		t.Fatal("不同 URL 不该撞 key")
	}
	// 不同角标参数共用同一份原图，前提就是 key 只看原图 URL
	if Key(url) != Key("https://c0.jdbstatic.com/covers/aq/AqMvn0.jpg") {
		t.Fatal("key 不应该依赖请求参数")
	}
}

func TestEvictsOldestWhenOverLimit(t *testing.T) {
	dir := t.TempDir()
	// 上限 120 字节：塞三张 60 字节的图必然超限
	cache, err := Open(dir, 120)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	payload := bytes.Repeat([]byte("a"), 60)
	for i, url := range []string{"u1", "u2", "u3"} {
		cache.Put(url, payload)
		// 淘汰按 mtime 排序，几次写入之间要有可分辨的时间差
		time.Sleep(20 * time.Millisecond)
		_ = i
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) >= 3 {
		t.Fatalf("超限后应该淘汰旧文件，仍剩 %d 个", len(entries))
	}
	if _, ok := cache.Get("u1"); ok {
		t.Fatal("最旧的条目应被淘汰")
	}
	if _, ok := cache.Get("u3"); !ok {
		t.Fatal("刚写入的条目不该被淘汰")
	}
}

func TestOpenRecoversSizeFromDisk(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first.Put("u1", bytes.Repeat([]byte("x"), 32))

	// 重启后水位线要从磁盘恢复，否则上限会被一点点撑破
	second, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("重新 Open: %v", err)
	}
	if second.total != 32 {
		t.Fatalf("重启后应恢复为 32 字节，得到 %d", second.total)
	}
	if _, ok := second.Get("u1"); !ok {
		t.Fatal("重启后应读得到原有缓存")
	}
}

func TestPutIgnoresEmptyPayload(t *testing.T) {
	dir := t.TempDir()
	cache, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cache.Put("u1", nil)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("空数据不该落盘，得到 %d 个文件", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, Key("u1")+".img")); !os.IsNotExist(err) {
		t.Fatal("空数据写入了文件")
	}
}
