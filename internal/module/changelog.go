package module

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	ref "github.com/distribution/reference"
	"github.com/zeromicro/go-zero/core/logx"
)

// ReleaseInfo 单条更新说明（releases 模式=一条 Release；commits 模式=一条提交）
type ReleaseInfo struct {
	TagName     string `json:"tagName"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	BodyZh      string `json:"bodyZh,omitempty"` // AI 中文翻译（配置 AI_API_KEY 后才有）
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
}

// ChangelogData 一个镜像对应源仓库的更新说明
type ChangelogData struct {
	Repo     string        `json:"repo"`
	Kind     string        `json:"kind"` // "releases" 或 "commits"
	Releases []ReleaseInfo `json:"releases"`
}

const sourceLabel = "org.opencontainers.image.source"

const changelogCacheTTL = 30 * time.Minute

const maxReleaseBodyLen = 4000

// ErrRepoNotFound 候选仓库在 GitHub 上不存在（或既无 Releases 也无提交），可尝试下一个候选
var ErrRepoNotFound = errors.New("github repo not found")

// knownImages 常见镜像的源仓库映射（镜像名 -> GitHub 仓库），用于无 label 时的兜底
var knownImages = map[string]string{
	"alist":                  "AlistGo/alist",
	"xhofe/alist":            "AlistGo/alist",
	"emby":                   "MediaBrowser/Emby.Releases",
	"embyserver":             "MediaBrowser/Emby.Releases",
	"emby/embyserver":        "MediaBrowser/Emby.Releases",
	"nginx":                  "nginxinc/docker-nginx",
	"mysql":                  "mysql/mysql-server",
	"redis":                  "redis/redis",
	"postgres":               "postgres/postgres",
	"caddy":                  "caddyserver/caddy",
	"traefik":                "traefik/traefik",
	"portainer":              "portainer/portainer",
	"portainer/portainer-ce": "portainer/portainer",
	"portainer/portainer-ee": "portainer/portainer",
	"vaultwarden":            "dani-garcia/vaultwarden",
	"vaultwarden/server":     "dani-garcia/vaultwarden",
	"gitea":                  "go-gitea/gitea",
	"gitea/gitea":            "go-gitea/gitea",
	"minio":                  "minio/minio",
	"minio/minio":            "minio/minio",
	"homeassistant":          "home-assistant/core",
	"nextcloud":              "nextcloud/server",
	"jellyswarm":             "jellyswarm/jellyswarm",
}

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
	return normalizeGitHubRepo(raw)
}

func normalizeGitHubRepo(raw string) string {
	raw = strings.TrimSpace(raw)
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

// SourceRepoCandidates 生成源仓库候选列表（按可信度排序）：
// 1. 用户手动指定的仓库（弹窗里填写，最高优先级）
// 2. 镜像 label 声明的仓库
// 3. ghcr.io 镜像路径通常等于 GitHub 仓库路径
// 4. 常见镜像映射表
// 5. Docker Hub 的 owner/name 猜测（作者名常与 GitHub 同名）；linuxserver 系列套用其命名规律
func SourceRepoCandidates(imageName string, labels map[string]string) []string {
	var candidates []string
	add := func(s string) {
		if s == "" {
			return
		}
		for _, c := range candidates {
			if c == s {
				return
			}
		}
		candidates = append(candidates, s)
	}

	add(UserRepoOverride(imageName))
	add(GetSourceRepo(labels))

	named, err := ref.ParseDockerRef(imageName)
	if err != nil {
		return candidates
	}
	domain, path := ref.Domain(named), ref.Path(named)
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case domain == "ghcr.io" && len(parts) >= 2:
		add(parts[0] + "/" + parts[1])
	case domain == "docker.io" || domain == "":
		switch {
		case len(parts) == 2:
			owner, name := parts[0], parts[1]
			if owner == "library" {
				add(knownImages[name])
			} else {
				add(knownImages[owner+"/"+name])
				if owner == "linuxserver" {
					add(owner + "/docker-" + name)
				}
				add(owner + "/" + name)
			}
		case len(parts) == 1:
			add(knownImages[parts[0]])
		}
	default:
		// 其他 registry（quay 等），路径可能同样是 owner/repo
		if len(parts) == 2 {
			add(parts[0] + "/" + parts[1])
		}
	}
	return candidates
}

// FetchChangelog 拉取仓库的更新说明：优先 GitHub Releases，仓库没发过 Releases 时
// 回退到最近的提交记录。结果缓存 30 分钟。
func FetchChangelog(repo string) (*ChangelogData, error) {
	if cached, ok := changelogCache.Load(repo); ok {
		entry := cached.(changelogCacheEntry)
		if time.Now().Before(entry.expireAt) {
			return &entry.data, nil
		}
	}

	data, err := fetchReleases(repo)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			// 仓库存在性未知时再试提交接口，两者都 404 才判定不存在
			commitData, commitErr := fetchCommits(repo)
			if commitErr != nil {
				return nil, ErrRepoNotFound
			}
			changelogCache.Store(repo, changelogCacheEntry{data: *commitData, expireAt: time.Now().Add(changelogCacheTTL)})
			return commitData, nil
		}
		return nil, err
	}
	if len(data.Releases) == 0 {
		commitData, commitErr := fetchCommits(repo)
		if commitErr != nil || len(commitData.Releases) == 0 {
			return nil, ErrRepoNotFound
		}
		changelogCache.Store(repo, changelogCacheEntry{data: *commitData, expireAt: time.Now().Add(changelogCacheTTL)})
		return commitData, nil
	}

	changelogCache.Store(repo, changelogCacheEntry{data: *data, expireAt: time.Now().Add(changelogCacheTTL)})
	return data, nil
}

var (
	htmlTableRe = regexp.MustCompile(`(?is)<table.*?</table>`)
	htmlTagRe   = regexp.MustCompile(`<[^>]*>`)
	blankLinesRe = regexp.MustCompile(`\n{3,}`)
)

// cleanReleaseBody 清理 Release 正文：很多项目往说明里塞下载链接表格和徽章图片，
// 这类 HTML 在弹窗里没法读。整块表格替换成提示，其余 HTML 标签剥掉，只留更新内容。
func cleanReleaseBody(body string) string {
	if body == "" {
		return ""
	}
	tableCount := len(htmlTableRe.FindAllString(body, -1))
	body = htmlTableRe.ReplaceAllString(body, "")
	body = htmlTagRe.ReplaceAllString(body, "")
	body = html.UnescapeString(body)
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = blankLinesRe.ReplaceAllString(body, "\n\n")
	body = strings.TrimSpace(body)
	if tableCount > 0 {
		if body != "" {
			body += "\n\n"
		}
		body += "（正文中的下载链接表格已省略，可点下方「在 GitHub 查看原文」）"
	}
	return body
}

func fetchReleases(repo string) (*ChangelogData, error) {
	apiURL := "https://api.github.com/repos/" + repo + "/releases?per_page=5"
	body, err := apiGet(apiURL)
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

	data := &ChangelogData{Repo: repo, Kind: "releases", Releases: []ReleaseInfo{}}
	for _, r := range releases {
		if r.Draft {
			continue
		}
		info := ReleaseInfo{
			TagName:     r.TagName,
			Name:        r.Name,
			Body:        cleanReleaseBody(r.Body),
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
	return data, nil
}

func fetchCommits(repo string) (*ChangelogData, error) {
	apiURL := "https://api.github.com/repos/" + repo + "/commits?per_page=10"
	body, err := apiGet(apiURL)
	if err != nil {
		return nil, err
	}

	var commits []struct {
		SHA    string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, fmt.Errorf("解析 GitHub 提交记录失败: %w", err)
	}

	data := &ChangelogData{Repo: repo, Kind: "commits", Releases: []ReleaseInfo{}}
	for _, c := range commits {
		message := strings.TrimSpace(c.Commit.Message)
		title := message
		if idx := strings.Index(message, "\n"); idx > 0 {
			title = message[:idx]
		}
		data.Releases = append(data.Releases, ReleaseInfo{
			TagName:     c.SHA[:min(7, len(c.SHA))],
			Name:        title,
			Body:        message,
			PublishedAt: c.Commit.Author.Date,
			URL:         c.HTMLURL,
		})
	}
	return data, nil
}

func apiGet(apiURL string) ([]byte, error) {
	proxied := apiURL
	if proxy := os.Getenv("githubProxy"); proxy != "" {
		proxied = strings.TrimRight(proxy, "/") + "/" + apiURL
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, proxied, nil)
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
			logx.Error("apiGet关闭body失败" + err.Error())
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	switch res.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, ErrRepoNotFound
	case http.StatusForbidden:
		return nil, fmt.Errorf("GitHub API 请求被限流(403)。未配置 GITHUB_TOKEN 时共享网络出口的限额(60次/小时)很容易耗尽，配置 GITHUB_TOKEN 环境变量后可提升到 5000 次/小时")
	default:
		return nil, fmt.Errorf("GitHub API 返回 %s: %s", res.Status, string(body))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
