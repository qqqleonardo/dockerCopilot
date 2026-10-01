package module

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// ReleaseInfo 单个 GitHub Release 的更新说明
type ReleaseInfo struct {
	TagName     string `json:"tagName"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
}

// ChangelogData 一个镜像对应源仓库的更新说明
type ChangelogData struct {
	Repo     string        `json:"repo"`
	Releases []ReleaseInfo `json:"releases"`
}

const sourceLabel = "org.opencontainers.image.source"

const changelogCacheTTL = 30 * time.Minute

const maxReleaseBodyLen = 4000

type changelogCacheEntry struct {
	data     ChangelogData
	expireAt time.Time
}

var changelogCache sync.Map

// GetSourceRepo 从镜像 label 中解析出 GitHub 仓库（owner/repo），非 GitHub 源返回空串
func GetSourceRepo(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	raw := strings.TrimSpace(labels[sourceLabel])
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	raw = strings.TrimPrefix(raw, "www.")
	if !strings.HasPrefix(strings.ToLower(raw), "github.com/") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(raw, "github.com/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// FetchChangelog 拉取仓库最近几条 GitHub Release 说明，结果缓存 30 分钟
func FetchChangelog(repo string) (*ChangelogData, error) {
	if cached, ok := changelogCache.Load(repo); ok {
		entry := cached.(changelogCacheEntry)
		if time.Now().Before(entry.expireAt) {
			return &entry.data, nil
		}
	}

	apiURL := "https://api.github.com/repos/" + repo + "/releases?per_page=5"
	if proxy := os.Getenv("githubProxy"); proxy != "" {
		apiURL = strings.TrimRight(proxy, "/") + "/" + apiURL
	}

	body, err := httpGetBody(apiURL)
	if err != nil {
		return nil, err
	}

	var releases []struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
		Draft       bool   `json:"draft"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 GitHub Releases 失败: %w", err)
	}

	data := ChangelogData{Repo: repo, Releases: []ReleaseInfo{}}
	for _, r := range releases {
		if r.Draft {
			continue
		}
		info := ReleaseInfo{
			TagName:     r.TagName,
			Name:        r.Name,
			Body:        r.Body,
			PublishedAt: r.PublishedAt,
			URL:         r.HTMLURL,
		}
		if info.Name == "" {
			info.Name = r.TagName
		}
		if len(info.Body) > maxReleaseBodyLen {
			info.Body = info.Body[:maxReleaseBodyLen] + "\n\n...(内容过长已截断)"
		}
		data.Releases = append(data.Releases, info)
	}

	changelogCache.Store(repo, changelogCacheEntry{data: data, expireAt: time.Now().Add(changelogCacheTTL)})
	return &data, nil
}

func httpGetBody(url string) ([]byte, error) {
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			logx.Error("FetchChangelog关闭body失败" + err.Error())
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("GitHub API 请求被限流(403)，可在部署时配置 GITHUB_TOKEN 环境变量提升限额，或配置 githubProxy 代理后重试")
		}
		return nil, fmt.Errorf("GitHub API 返回 %s: %s", res.Status, string(body))
	}
	return body, nil
}
