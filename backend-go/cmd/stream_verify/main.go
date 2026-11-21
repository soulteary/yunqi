// stream_verify - 对比验证流式响应的 Token 统计和事件格式
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/soulteary/yunqi/internal/httpclient"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/soulteary/yunqi/internal/errutil"
)

type UsageInfo struct {
	InputTokens              interface{} `json:"input_tokens"`
	OutputTokens             interface{} `json:"output_tokens"`
	CacheCreationInputTokens interface{} `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     interface{} `json:"cache_read_input_tokens,omitempty"`
}

type EventInfo struct {
	Type    string                 `json:"type"`
	Message map[string]interface{} `json:"message,omitempty"`
	Usage   *UsageInfo             `json:"usage,omitempty"`
	Delta   map[string]interface{} `json:"delta,omitempty"`
	Error   map[string]interface{} `json:"error,omitempty"`
	Index   int                    `json:"index,omitempty"`
}

type VerifyResult struct {
	Name            string
	EventTypes      []string
	UsageEvents     []UsageInfo
	FinalUsage      *UsageInfo
	ContentLength   int
	EventCount      int
	HasMessageStart bool
	HasMessageStop  bool
	HasContentBlock bool
	Errors          []string
	FirstByteMs     int64
	TotalMs         int64
	RawContent      string
}

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	flags := flag.NewFlagSet("stream_verify", flag.ContinueOnError)
	proxyURL := flags.String("proxy", "http://localhost:3000", "代理服务器地址")
	upstreamURL := flags.String("upstream", "", "上游服务器地址（用于对比）")
	proxyKey := flags.String("proxy-key", "", "代理 API Key（也可使用 PROXY_ACCESS_KEY）")
	upstreamKey := flags.String("upstream-key", "", "上游 API Key（也可使用 UPSTREAM_API_KEY）")
	model := flags.String("model", "", "模型名称（必填，诊断会真实调用并消耗额度）")
	prompt := flags.String("prompt", "说一个简短的笑话", "测试 prompt")
	verbose := flags.Bool("v", false, "显示详细事件和响应内容")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Println("错误: 不接受位置参数")
		return 2
	}
	if *proxyKey == "" {
		*proxyKey = os.Getenv("PROXY_ACCESS_KEY")
	}
	if *upstreamKey == "" {
		*upstreamKey = os.Getenv("UPSTREAM_API_KEY")
	}
	if *proxyKey == "" || strings.TrimSpace(*model) == "" {
		fmt.Println("错误: 需要代理 API Key 和显式 -model 参数")
		return 1
	}
	if _, err := streamURL(*proxyURL); err != nil {
		fmt.Printf("错误: 代理地址无效: %v\n", err)
		return 1
	}
	if *upstreamURL != "" {
		if *upstreamKey == "" {
			fmt.Println("错误: 指定上游地址时需要上游 API Key")
			return 1
		}
		if _, err := streamURL(*upstreamURL); err != nil {
			fmt.Printf("错误: 上游地址无效: %v\n", err)
			return 1
		}
	}
	fmt.Println("========== 测试代理服务器 ==========")
	proxyResult := verifyStream("代理", *proxyURL, *proxyKey, *model, *prompt, *verbose)
	printResult(proxyResult)
	success := len(proxyResult.Errors) == 0
	if *upstreamURL != "" {
		fmt.Println("\n========== 测试上游服务器 ==========")
		upstreamResult := verifyStream("上游", *upstreamURL, *upstreamKey, *model, *prompt, *verbose)
		printResult(upstreamResult)
		fmt.Println("\n========== 对比分析 ==========")
		compareResults(proxyResult, upstreamResult, *verbose, *proxyKey, *upstreamKey)
		success = resultsMatch(proxyResult, upstreamResult)
	}
	if !success {
		return 1
	}
	return 0
}

func streamURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("需要有效的 HTTP(S) 服务器地址")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func verifyStream(name, baseURL, apiKey, model, prompt string, verbose bool) *VerifyResult {
	result := &VerifyResult{Name: name}
	defer func() {
		for i := range result.Errors {
			result.Errors[i] = redactText(result.Errors[i], apiKey)
		}
	}()
	startTime := time.Now()

	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 500,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	endpoint, err := streamURL(baseURL)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("创建请求失败: %v", err))
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// 使用 Go 默认 gzip 协商与解压，不声明未实现的 deflate/br 支持。
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "claude-code-20250219,interleaved-thinking-2025-05-14")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("User-Agent", "claude-cli/2.0.74 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Package-Version", "0.70.0")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Runtime-Version", "v24.3.0")
	req.Header.Set("X-Stainless-Helper-Method", "stream")
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Timeout", "200")

	client := httpclient.GetManager().GetStandardClient(60*time.Second, false)
	resp, err := client.Do(req)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("请求失败: %v", err))
		return result
	}
	defer errutil.IgnoreDeferred(resp.Body.Close)

	var body io.Reader = resp.Body
	switch encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); encoding {
	case "", "identity":
	case "gzip":
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("解压响应失败: %v", err))
			return result
		}
		defer errutil.IgnoreDeferred(reader.Close)
		body = reader
	default:
		result.Errors = append(result.Errors, "不支持的响应压缩: "+encoding)
		return result
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(body, 4096))
		result.Errors = append(result.Errors, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(data)))
		return result
	}
	var firstByte bool
	var contentBuf strings.Builder
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var cumulative UsageInfo
	consume := func(jsonStr string) {
		if jsonStr == "" || jsonStr == "[DONE]" {
			return
		}
		result.EventCount++
		var event EventInfo
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("JSON解析失败: %s", jsonStr[:min(100, len(jsonStr))]))
			return
		}
		result.EventTypes = append(result.EventTypes, event.Type)
		if verbose {
			fmt.Printf("[%s][%s] %s\n", name, event.Type, redactText(jsonStr, apiKey))
		}
		if event.Type == "message_start" && event.Message != nil {
			if usage, ok := event.Message["usage"].(map[string]interface{}); ok {
				u := extractUsage(usage)
				mergeUsage(&cumulative, u)
				result.UsageEvents = append(result.UsageEvents, u)
			}
		}
		if event.Usage != nil {
			mergeUsage(&cumulative, *event.Usage)
			result.UsageEvents = append(result.UsageEvents, *event.Usage)
		}
		switch event.Type {
		case "error":
			message := "上游返回 error 事件"
			if detail, ok := event.Error["message"].(string); ok && detail != "" {
				message += ": " + detail
			}
			result.Errors = append(result.Errors, message)
		case "message_start":
			result.HasMessageStart = true
		case "message_stop":
			result.HasMessageStop = true
		case "content_block_start":
			result.HasContentBlock = true
		case "content_block_delta":
			if text, ok := event.Delta["text"].(string); ok {
				contentBuf.WriteString(text)
			}
		case "message_delta":
			if event.Usage != nil {
				snapshot := cumulative
				result.FinalUsage = &snapshot
			}
		}
	}
	var dataLines []string
	dispatch := func() {
		if len(dataLines) > 0 {
			consume(strings.Join(dataLines, "\n"))
		}
		dataLines = nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !firstByte {
			result.FirstByteMs = time.Since(startTime).Milliseconds()
			firstByte = true
		}
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	dispatch()
	if err := scanner.Err(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("读取流失败: %v", err))
	}
	result.TotalMs = time.Since(startTime).Milliseconds()
	result.ContentLength = contentBuf.Len()
	result.RawContent = contentBuf.String()

	// 验证检查
	if !result.HasMessageStart {
		result.Errors = append(result.Errors, "缺少 message_start 事件")
	}
	if !result.HasMessageStop {
		result.Errors = append(result.Errors, "缺少 message_stop 事件")
	}
	if !result.HasContentBlock {
		result.Errors = append(result.Errors, "缺少 content_block_start 事件")
	}
	if result.FinalUsage == nil {
		result.Errors = append(result.Errors, "缺少最终 usage 数据")
	} else {
		for _, field := range []struct {
			name     string
			value    interface{}
			required bool
		}{
			{"input_tokens", result.FinalUsage.InputTokens, true},
			{"output_tokens", result.FinalUsage.OutputTokens, true},
			{"cache_creation_input_tokens", result.FinalUsage.CacheCreationInputTokens, false},
			{"cache_read_input_tokens", result.FinalUsage.CacheReadInputTokens, false},
		} {
			if field.value == nil && !field.required {
				continue
			}
			if _, ok := tokenCount(field.value); !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("%s 必须为非负整数: %v", field.name, field.value))
			}
		}
	}
	if result.ContentLength == 0 {
		result.Errors = append(result.Errors, "响应内容为空")
	}

	return result
}

func extractUsage(m map[string]interface{}) UsageInfo {
	return UsageInfo{
		InputTokens:              m["input_tokens"],
		OutputTokens:             m["output_tokens"],
		CacheCreationInputTokens: m["cache_creation_input_tokens"],
		CacheReadInputTokens:     m["cache_read_input_tokens"],
	}
}

func mergeUsage(target *UsageInfo, delta UsageInfo) {
	if delta.InputTokens != nil {
		target.InputTokens = delta.InputTokens
	}
	if delta.OutputTokens != nil {
		target.OutputTokens = delta.OutputTokens
	}
	if delta.CacheCreationInputTokens != nil {
		target.CacheCreationInputTokens = delta.CacheCreationInputTokens
	}
	if delta.CacheReadInputTokens != nil {
		target.CacheReadInputTokens = delta.CacheReadInputTokens
	}
}

func tokenCount(value interface{}) (int64, bool) {
	switch n := value.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(math.MaxInt64) || math.Trunc(n) != n {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), n >= 0
	case int64:
		return n, n >= 0
	}
	return 0, false
}

func toInt(value interface{}) int64 {
	count, _ := tokenCount(value)
	return count
}

func resultsMatch(proxy, upstream *VerifyResult) bool {
	if len(proxy.Errors) != 0 || len(upstream.Errors) != 0 || proxy.FinalUsage == nil || upstream.FinalUsage == nil || proxy.RawContent != upstream.RawContent {
		return false
	}
	return toInt(proxy.FinalUsage.InputTokens) == toInt(upstream.FinalUsage.InputTokens) &&
		toInt(proxy.FinalUsage.OutputTokens) == toInt(upstream.FinalUsage.OutputTokens) &&
		toInt(proxy.FinalUsage.CacheCreationInputTokens) == toInt(upstream.FinalUsage.CacheCreationInputTokens) &&
		toInt(proxy.FinalUsage.CacheReadInputTokens) == toInt(upstream.FinalUsage.CacheReadInputTokens)
}

func printResult(r *VerifyResult) {
	fmt.Printf("\n--- %s ---\n", r.Name)
	fmt.Printf("事件总数: %d\n", r.EventCount)
	fmt.Printf("首字节延迟: %dms\n", r.FirstByteMs)
	fmt.Printf("总耗时: %dms\n", r.TotalMs)
	fmt.Printf("响应内容长度: %d 字符\n", r.ContentLength)

	fmt.Println("\n事件完整性:")
	fmt.Printf("  message_start: %v\n", r.HasMessageStart)
	fmt.Printf("  content_block: %v\n", r.HasContentBlock)
	fmt.Printf("  message_stop: %v\n", r.HasMessageStop)

	fmt.Println("\nUsage 统计:")
	if r.FinalUsage != nil {
		fmt.Printf("  input_tokens: %v\n", r.FinalUsage.InputTokens)
		fmt.Printf("  output_tokens: %v\n", r.FinalUsage.OutputTokens)
		if r.FinalUsage.CacheCreationInputTokens != nil {
			fmt.Printf("  cache_creation: %v\n", r.FinalUsage.CacheCreationInputTokens)
		}
		if r.FinalUsage.CacheReadInputTokens != nil {
			fmt.Printf("  cache_read: %v\n", r.FinalUsage.CacheReadInputTokens)
		}
	} else {
		fmt.Println("  无 usage 数据!")
	}

	fmt.Println("\n事件类型统计:")
	typeCount := make(map[string]int)
	for _, t := range r.EventTypes {
		typeCount[t]++
	}
	for t, c := range typeCount {
		fmt.Printf("  %s: %d\n", t, c)
	}

	if len(r.Errors) > 0 {
		fmt.Println("\n❌ 发现问题:")
		for _, e := range r.Errors {
			fmt.Printf("  • %s\n", e)
		}
	} else {
		fmt.Println("\n✅ 验证通过")
	}
}

func compareResults(proxy, upstream *VerifyResult, verbose bool, secrets ...string) {
	fmt.Println("\n--- 性能对比 ---")
	fmt.Printf("首字节延迟: 代理 %dms vs 上游 %dms (差值: %+dms)\n",
		proxy.FirstByteMs, upstream.FirstByteMs, proxy.FirstByteMs-upstream.FirstByteMs)
	fmt.Printf("总耗时: 代理 %dms vs 上游 %dms (差值: %+dms)\n",
		proxy.TotalMs, upstream.TotalMs, proxy.TotalMs-upstream.TotalMs)

	fmt.Println("\n--- Token 统计对比 ---")
	if proxy.FinalUsage != nil && upstream.FinalUsage != nil {
		proxyInput := toInt(proxy.FinalUsage.InputTokens)
		upstreamInput := toInt(upstream.FinalUsage.InputTokens)
		proxyOutput := toInt(proxy.FinalUsage.OutputTokens)
		upstreamOutput := toInt(upstream.FinalUsage.OutputTokens)

		fmt.Printf("input_tokens: 代理 %d vs 上游 %d", proxyInput, upstreamInput)
		if proxyInput != upstreamInput {
			fmt.Printf(" ⚠️ 不一致 (差值: %+d)\n", proxyInput-upstreamInput)
		} else {
			fmt.Println(" ✅")
		}

		fmt.Printf("output_tokens: 代理 %d vs 上游 %d", proxyOutput, upstreamOutput)
		if proxyOutput != upstreamOutput {
			fmt.Printf(" ⚠️ 不一致 (差值: %+d)\n", proxyOutput-upstreamOutput)
		} else {
			fmt.Println(" ✅")
		}
	} else {
		if proxy.FinalUsage == nil {
			fmt.Println("⚠️ 代理缺少 usage 数据")
		}
		if upstream.FinalUsage == nil {
			fmt.Println("⚠️ 上游缺少 usage 数据")
		}
	}

	for _, field := range []struct {
		name            string
		proxy, upstream interface{}
	}{
		{"cache_creation_input_tokens", proxy.FinalUsageValue(true), upstream.FinalUsageValue(true)},
		{"cache_read_input_tokens", proxy.FinalUsageValue(false), upstream.FinalUsageValue(false)},
	} {
		fmt.Printf("%s: 代理 %d vs 上游 %d\n", field.name, toInt(field.proxy), toInt(field.upstream))
	}
	fmt.Println("\n--- 内容对比 ---")
	fmt.Printf("内容长度: 代理 %d vs 上游 %d", proxy.ContentLength, upstream.ContentLength)
	if proxy.ContentLength != upstream.ContentLength {
		fmt.Printf(" ⚠️ 不一致 (差值: %+d)\n", proxy.ContentLength-upstream.ContentLength)
	} else {
		fmt.Println(" ✅")
	}

	if proxy.RawContent == upstream.RawContent {
		fmt.Println("内容完全一致 ✅")
	} else {
		fmt.Println("内容不一致 ⚠️")
		if verbose {
			fmt.Println("\n代理响应内容:")
			fmt.Println(redactText(proxy.RawContent, secrets...))
			fmt.Println("\n上游响应内容:")
			fmt.Println(redactText(upstream.RawContent, secrets...))
		}
	}

	fmt.Println("\n--- 事件序列对比 ---")
	proxyTypes := strings.Join(proxy.EventTypes, " → ")
	upstreamTypes := strings.Join(upstream.EventTypes, " → ")
	if proxyTypes == upstreamTypes {
		fmt.Println("事件序列一致 ✅")
	} else {
		fmt.Println("事件序列不一致 ⚠️")
		fmt.Printf("代理: %s\n", proxyTypes)
		fmt.Printf("上游: %s\n", upstreamTypes)
	}

	fmt.Println("\n--- 问题汇总 ---")
	allGood := resultsMatch(proxy, upstream)

	if allGood {
		fmt.Println("✅ 代理与上游内容和 Token 统计一致，无问题")
	} else {
		fmt.Println("❌ 验证失败或响应/Token 统计不一致")
		if len(proxy.Errors) > 0 {
			fmt.Println("\n代理问题:")
			for _, e := range proxy.Errors {
				fmt.Printf("  • %s\n", e)
			}
		}
		if len(upstream.Errors) > 0 {
			fmt.Println("\n上游问题:")
			for _, e := range upstream.Errors {
				fmt.Printf("  • %s\n", e)
			}
		}
	}
}

func (r *VerifyResult) FinalUsageValue(creation bool) interface{} {
	if r.FinalUsage == nil {
		return nil
	}
	if creation {
		return r.FinalUsage.CacheCreationInputTokens
	}
	return r.FinalUsage.CacheReadInputTokens
}

func redactText(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}
