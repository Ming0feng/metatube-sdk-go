package engine

import (
	"context"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// ProviderConnectivity 描述单个 provider 的连通性探测结果。
type ProviderConnectivity struct {
	Type      string `json:"type"`             // movie / actor
	Provider  string `json:"provider"`         // provider 名称
	URL       string `json:"url"`              // 探测的 baseURL
	OK        bool   `json:"ok"`               // 网络层是否可达（有响应即视为可达）
	Status    int    `json:"status,omitempty"` // HTTP 状态码（可达时）
	Error     string `json:"error,omitempty"`  // 网络层错误（不可达时）
	ElapsedMS int64  `json:"elapsed_ms"`       // 探测耗时（毫秒）
}

// connectivityTimeout 是单次探测的超时时间。
const connectivityTimeout = 10 * time.Second

// CheckConnectivity 并发探测全部 movie / actor provider 的 baseURL 连通性。
// 单个 provider 的失败只体现在其结果里，不影响其它 provider。
func (e *Engine) CheckConnectivity(ctx context.Context) []ProviderConnectivity {
	client := &http.Client{Timeout: connectivityTimeout}

	type probe struct {
		typ    string
		name   string
		rawURL string
	}
	probes := make([]probe, 0, e.movieProviders.Len()+e.actorProviders.Len())
	for _, p := range e.movieProviders.Iterator() {
		probes = append(probes, probe{"movie", p.Name(), p.URL().String()})
	}
	for _, p := range e.actorProviders.Iterator() {
		probes = append(probes, probe{"actor", p.Name(), p.URL().String()})
	}

	results := make([]ProviderConnectivity, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p probe) {
			defer wg.Done()
			results[i] = checkOne(ctx, client, p.typ, p.name, p.rawURL)
		}(i, p)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type < results[j].Type
		}
		return results[i].Provider < results[j].Provider
	})
	return results
}

func checkOne(ctx context.Context, client *http.Client, typ, name, rawURL string) ProviderConnectivity {
	result := ProviderConnectivity{Type: typ, Provider: name, URL: rawURL}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MetaTube/connectivity)")

	start := time.Now()
	resp, err := client.Do(req)
	result.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	// 读到少量内容即确认连接可用，随后丢弃。
	_, _ = io.CopyN(io.Discard, resp.Body, 1024)
	result.OK = true
	result.Status = resp.StatusCode
	return result
}
