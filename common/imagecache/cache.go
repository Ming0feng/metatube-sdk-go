// Package imagecache 把下载过的原图字节落到磁盘。
//
// 存在的理由：海报上的角标（中文字幕 / 无码破解）是**请求时**才叠的，
// 每次都要先去 javdb / javbus 下载原图。代理一抖，整张图就 500，
// Emby 那边只看到一张空白海报。
//
// 缓存的是**原图**而不是成品：叠哪几个角标、贴哪个位置、裁多少，
// 都是请求参数决定的（插件里随时能开关）。缓存成品会导致
// "明明关了角标，出来的还是旧图"；缓存原图则一份数据能拼出所有变体，
// 断网时也能本地重算。
package imagecache

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// DefaultMaxBytes 是缓存目录的默认上限（1 GiB）。
// 一张封面通常 100~400 KiB，1 GiB 大约够存几千部，对媒体库来说够用。
const DefaultMaxBytes int64 = 1 << 30

// Cache 是并发安全的磁盘缓存。零值不可用，请用 Open 构造。
type Cache struct {
	dir      string
	maxBytes int64

	mu    sync.Mutex
	total int64
}

// Open 打开（必要时创建）缓存目录。
//
// dir 为空表示显式禁用，此时返回 (nil, nil) —— 调用方直接跳过缓存即可，
// 不算错误。目录不可创建（权限、只读挂载）才返回 error，
// 由调用方决定是否降级为"无缓存"。
func Open(dir string, maxBytes int64) (*Cache, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	c := &Cache{dir: dir, maxBytes: maxBytes}
	c.total = c.sizeOnDisk()
	return c, nil
}

// Dir 返回缓存目录（调试/日志用）。
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// Key 把 URL 映射成稳定的缓存键。
func Key(url string) string {
	sum := sha1.Sum([]byte(url))
	return hex.EncodeToString(sum[:])
}

// Get 读取缓存。未命中、文件损坏、无缓存都返回 ok=false。
func (c *Cache) Get(url string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	data, err := os.ReadFile(c.path(Key(url)))
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put 写入缓存。
//
// 所有错误都吞掉：缓存是锦上添花，写不进去最多是下次再下载一遍，
// 绝不能因为它让一张本来能出的图变成 500。
func (c *Cache) Put(url string, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	dst := c.path(Key(url))
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	// 同一张图被并发请求时会重复写，先扣掉旧文件，否则水位线越算越高、淘汰来得莫名其妙
	if info, err := os.Stat(dst); err == nil {
		c.total -= info.Size()
	}
	// rename 是原子的：读侧不会看到写了一半的图
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return
	}

	c.total += int64(len(data))
	if c.total > c.maxBytes {
		c.evictLocked()
	}
}

// Clear 删除缓存目录内的所有文件，并把水位线归零。
//
// 供外部（如元数据整理任务）主动失效整份缓存：整理完成后原图已落地到
// 媒体目录的 poster.jpg，服务端没必要再留一份。
// 删除失败的文件会跳过并返回第一个错误；返回值为成功删除的文件数。
func (c *Cache) Clear() (int, error) {
	if c == nil {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	items, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			c.total = 0
			return 0, nil
		}
		return 0, err
	}

	var (
		removed  int
		firstErr error
	)
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, item.Name())); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}

	// 以磁盘实际情况为准，避免删除失败时水位线虚低。
	c.total = c.sizeOnDisk()
	return removed, firstErr
}

// evictLocked 按最后写入时间淘汰旧图，直到降到上限的 80%。
// 调用方必须持有 c.mu。
func (c *Cache) evictLocked() {
	type entry struct {
		path    string
		size    int64
		modTime int64
	}

	items, err := os.ReadDir(c.dir)
	if err != nil {
		c.total = 0
		return
	}

	entries := make([]entry, 0, len(items))
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		entries = append(entries, entry{
			path:    filepath.Join(c.dir, item.Name()),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
	}

	// 最旧的先删
	sort.Slice(entries, func(i, j int) bool { return entries[i].modTime < entries[j].modTime })

	target := c.maxBytes * 8 / 10
	current := c.total
	for _, e := range entries {
		if current <= target {
			break
		}
		if err := os.Remove(e.path); err == nil {
			current -= e.size
		}
	}
	c.total = current
}

// sizeOnDisk 统计现有缓存大小（重启后恢复水位线）。
func (c *Cache) sizeOnDisk() int64 {
	var total int64
	items, err := os.ReadDir(c.dir)
	if err != nil {
		return 0
	}
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if info, err := item.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key+".img")
}
