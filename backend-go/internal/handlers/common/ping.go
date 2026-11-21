package common

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/errutil"
	"github.com/soulteary/yunqi/internal/httpclient"
)

type PingRequestBuilder func(upstream config.UpstreamConfig, baseURL string) (*http.Request, error)

type pingResult struct {
	latency int64
	success bool
	err     string
}

func PingSingleBaseURLUpstream(upstream config.UpstreamConfig, builder PingRequestBuilder) fiber.Map {
	baseURL := upstream.GetEffectiveBaseURL()
	if baseURL == "" {
		return fiber.Map{"success": false, "error": "No base URL configured", "latency": 0, "status": "error"}
	}
	return pingBaseURL(upstream, baseURL, 10*time.Second, builder)
}

func PingAllSingleBaseURLUpstreams(upstreams []config.UpstreamConfig, builder PingRequestBuilder, wrapInChannels bool) fiber.Map {
	results := make([]fiber.Map, len(upstreams))
	var wg sync.WaitGroup
	for i, upstream := range upstreams {
		wg.Add(1)
		go func(index int, up config.UpstreamConfig) {
			defer wg.Done()
			result := PingSingleBaseURLUpstream(up, builder)
			result["id"] = index
			result["index"] = index
			result["name"] = up.Name
			results[index] = result
		}(i, upstream)
	}
	wg.Wait()
	if wrapInChannels {
		return fiber.Map{"channels": results}
	}
	return fiber.Map{"results": results}
}

func PingMultiBaseURLUpstream(upstream config.UpstreamConfig, builder PingRequestBuilder) fiber.Map {
	urls := upstream.GetAllBaseURLs()
	if len(urls) == 0 {
		return fiber.Map{"success": false, "latency": 0, "status": "error", "error": "no_base_url"}
	}
	if len(urls) == 1 {
		return pingBaseURL(upstream, urls[0], 5*time.Second, builder)
	}

	results := make(chan pingResult, len(urls))
	for _, baseURL := range urls {
		go func(url string) {
			res := pingBaseURL(upstream, url, 5*time.Second, builder)
			results <- pingResultFromPayload(res)
		}(baseURL)
	}

	var best *pingResult
	for i := 0; i < len(urls); i++ {
		res := <-results
		if res.success {
			if best == nil || !best.success || res.latency < best.latency {
				copy := res
				best = &copy
			}
		} else if best == nil || !best.success {
			copy := res
			best = &copy
		}
	}

	if best == nil {
		return fiber.Map{"success": false, "latency": 0, "status": "error", "error": "all_urls_failed"}
	}
	if best.success {
		return fiber.Map{"success": true, "latency": best.latency, "status": "healthy"}
	}
	return fiber.Map{"success": false, "latency": best.latency, "status": "error", "error": best.err}
}

func PingAllMultiBaseURLUpstreams(upstreams []config.UpstreamConfig, builder PingRequestBuilder) []fiber.Map {
	results := make(chan fiber.Map, len(upstreams))
	var wg sync.WaitGroup
	for i, upstream := range upstreams {
		wg.Add(1)
		go func(id int, up config.UpstreamConfig) {
			defer wg.Done()
			result := PingMultiBaseURLUpstream(up, builder)
			result["id"] = id
			result["name"] = up.Name
			results <- result
		}(i, upstream)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	finalResults := make([]fiber.Map, 0, len(upstreams))
	for res := range results {
		finalResults = append(finalResults, res)
	}
	return finalResults
}

func pingBaseURL(upstream config.UpstreamConfig, baseURL string, timeout time.Duration, builder PingRequestBuilder) fiber.Map {
	client := httpclient.GetManager().GetClient(httpclient.ClientOptions{
		Timeout:           timeout,
		Insecure:          upstream.InsecureSkipVerify,
		ProxyURL:          upstream.ProxyURL,
		ProxyPreferDirect: upstream.ProxyPreferDirect,
		OutboundNetwork:   upstream.OutboundNetwork,
	})
	req, err := builder(upstream, strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return fiber.Map{"success": false, "error": "req_creation_failed", "latency": 0, "status": "error"}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return fiber.Map{"success": false, "error": err.Error(), "latency": latency, "status": "error"}
	}
	defer errutil.IgnoreDeferred(resp.Body.Close)

	status := "error"
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		status = "healthy"
	}
	return fiber.Map{
		"success":    resp.StatusCode >= 200 && resp.StatusCode < 400,
		"statusCode": resp.StatusCode,
		"latency":    latency,
		"status":     status,
	}
}

func pingResultFromPayload(payload fiber.Map) pingResult {
	result := pingResult{}
	if latency, ok := payload["latency"].(int64); ok {
		result.latency = latency
	} else if latency, ok := payload["latency"].(float64); ok {
		result.latency = int64(latency)
	}
	if success, ok := payload["success"].(bool); ok {
		result.success = success
	}
	if err, ok := payload["error"].(string); ok {
		result.err = err
	}
	return result
}
