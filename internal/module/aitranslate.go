package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// 可选的 AI 中文翻译：接任意 OpenAI 兼容的 chat/completions 接口。
// 配置 AI_API_KEY 即启用；默认智谱 glm-4-flash（免费、国内直连），可换 DeepSeek 等。
// 翻译结果按 仓库|tag 永久缓存到数据目录——同一版本的更新说明不会变，只花一次调用。

const (
	defaultAIBaseURL = "https://open.bigmodel.cn/api/paas/v4"
	defaultAIModel   = "glm-4-flash"

	maxPromptBodyLen = 2000
	aiCacheTTL       = 0 // 永久
)

var aiCacheFile = func() string {
	if f := os.Getenv("AI_CACHE_FILE"); f != "" {
		return f
	}
	return "/data/ai_cache.json"
}()

var aiCache sync.Map // key: repo|tag -> 中文内容

var aiCacheLoaded sync.Once

var aiTagRe = regexp.MustCompile(`(?s)<<<TAG\s*(.+?)\s*>>>`)

func AIEnabled() bool {
	return os.Getenv("AI_API_KEY") != ""
}

func aiBaseURL() string {
	if u := os.Getenv("AI_BASE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return defaultAIBaseURL
}

func aiModel() string {
	if m := os.Getenv("AI_MODEL"); m != "" {
		return m
	}
	return defaultAIModel
}

func loadAICache() {
	aiCacheLoaded.Do(func() {
		data, err := os.ReadFile(aiCacheFile)
		if err != nil {
			return
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			logx.Errorf("解析 AI 缓存文件失败(%s): %v", aiCacheFile, err)
			return
		}
		for k, v := range m {
			aiCache.Store(k, v)
		}
	})
}

func aiCacheGet(key string) (string, bool) {
	loadAICache()
	v, ok := aiCache.Load(key)
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

func aiCachePut(key, value string) {
	loadAICache()
	aiCache.Store(key, value)
	go persistAICache(key, value)
}

func persistAICache(newKey, newValue string) {
	m := map[string]string{}
	if data, err := os.ReadFile(aiCacheFile); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	m[newKey] = newValue
	if err := os.MkdirAll(dirOf(aiCacheFile), 0o755); err != nil {
		logx.Errorf("创建AI缓存目录失败: %v", err)
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(aiCacheFile, data, 0o644); err != nil {
		logx.Errorf("写入AI缓存文件失败: %v", err)
	}
}

func dirOf(path string) string {
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx]
	}
	if idx := strings.LastIndex(path, "\\"); idx > 0 {
		return path[:idx]
	}
	return "."
}

// TranslateBatch 把一批更新说明翻译成中文，返回 tag -> 中文 的映射。
// 缓存命中的不重复翻译；失败返回 error，由调用方决定降级为原文。
func TranslateBatch(repo string, items []ReleaseInfo) (map[string]string, error) {
	var missing []ReleaseInfo
	result := map[string]string{}
	for _, item := range items {
		key := repo + "|" + item.TagName
		if zh, ok := aiCacheGet(key); ok && zh != "" {
			result[item.TagName] = zh
		} else {
			missing = append(missing, item)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}

	translated, err := callAI(missing)
	if err != nil {
		return result, err
	}
	for tag, zh := range translated {
		if zh == "" {
			continue
		}
		result[tag] = zh
		aiCachePut(repo+"|"+tag, zh)
	}
	return result, nil
}

func callAI(items []ReleaseInfo) (map[string]string, error) {
	var b strings.Builder
	for _, item := range items {
		body := item.Body
		if len(body) > maxPromptBodyLen {
			body = body[:maxPromptBodyLen]
		}
		fmt.Fprintf(&b, "[TAG] %s\n%s\n\n", item.TagName, body)
	}

	systemPrompt := "你是 Docker 镜像更新说明的翻译助手。把给出的每条更新说明分别翻译成简体中文：" +
		"保留版本号、代码、命令、链接、专有名词原样；内容简短就照实翻译；忽略下载链接和徽章。" +
		"必须严格按照用户要求的格式输出，除翻译内容外不要输出任何解释。"

	userPrompt := "请将下面每条更新说明翻译成简体中文。每条必须严格按此格式输出（TAG 原样保留）：\n" +
		"<<<TAG 版本号>>>\n该条说明的中文翻译\n\n" +
		"以下是各条说明：\n\n" + b.String()

	reqBody := map[string]interface{}{
		"model":       aiModel(),
		"temperature": 0.1,
		"max_tokens":  4000,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		Timeout:   60 * time.Second,
	}
	req, err := http.NewRequest(http.MethodPost, aiBaseURL()+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("AI_API_KEY"))

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			logx.Error("callAI关闭body失败" + err.Error())
		}
	}()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI 接口返回 %s: %s", res.Status, truncateStr(string(respBody), 200))
	}

	var aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &aiResp); err != nil {
		return nil, fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if len(aiResp.Choices) == 0 {
		return nil, fmt.Errorf("AI 响应没有内容")
	}

	return parseAITranslations(aiResp.Choices[0].Message.Content, items), nil
}

func parseAITranslations(content string, items []ReleaseInfo) map[string]string {
	result := map[string]string{}
	matches := aiTagRe.FindAllStringSubmatchIndex(content, -1)
	for i, m := range matches {
		tag := strings.TrimSpace(content[m[2]:m[3]])
		var text string
		if i+1 < len(matches) {
			text = strings.TrimSpace(content[m[1]:matches[i+1][0]])
		} else {
			text = strings.TrimSpace(content[m[1]:])
		}
		// 校验 tag 确实在请求列表里，防止模型编造
		for _, item := range items {
			if item.TagName == tag {
				result[tag] = text
				break
			}
		}
	}
	return result
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
