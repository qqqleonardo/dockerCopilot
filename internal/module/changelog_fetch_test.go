package module

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFetchChangelogWithMockServer(t *testing.T) {
	sample := `[
		{
			"tag_name": "v4.6.7",
			"name": "qBittorrent 4.6.7",
			"body": "## 改动内容\n- 修复了一个 bug\n- 支持 ` + "`WEBUI`" + ` 新特性\n\n**升级提示**: 请备份数据",
			"published_at": "2026-08-15T10:00:00Z",
			"html_url": "https://github.com/linuxserver/docker-qbittorrent/releases/tag/v4.6.7",
			"draft": false
		},
		{
			"tag_name": "v4.6.6",
			"name": "",
			"body": "minor fix",
			"published_at": "2026-07-01T08:00:00Z",
			"html_url": "https://github.com/linuxserver/docker-qbittorrent/releases/tag/v4.6.6",
			"draft": false
		},
		{
			"tag_name": "v4.6.5-draft",
			"name": "draft release",
			"body": "not published yet",
			"published_at": "2026-06-01T08:00:00Z",
			"html_url": "https://github.com/linuxserver/docker-qbittorrent/releases/tag/v4.6.5-draft",
			"draft": true
		}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing Accept header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sample))
	}))
	defer server.Close()

	t.Setenv("githubProxy", server.URL)
	// 清掉可能存在的缓存（同进程内其他测试可能写入过）
	changelogCache.Delete("linuxserver/docker-qbittorrent")

	data, err := FetchChangelog("linuxserver/docker-qbittorrent")
	if err != nil {
		t.Fatalf("FetchChangelog returned error: %v", err)
	}

	if data.Repo != "linuxserver/docker-qbittorrent" {
		t.Errorf("repo = %q", data.Repo)
	}
	// draft 应被过滤
	if len(data.Releases) != 2 {
		t.Fatalf("expected 2 releases (draft filtered), got %d", len(data.Releases))
	}

	first := data.Releases[0]
	if first.TagName != "v4.6.7" || first.Name != "qBittorrent 4.6.7" {
		t.Errorf("first release = %+v", first)
	}
	if first.URL != "https://github.com/linuxserver/docker-qbittorrent/releases/tag/v4.6.7" {
		t.Errorf("first release url = %q", first.URL)
	}
	second := data.Releases[1]
	if second.Name != "v4.6.6" {
		t.Errorf("empty name should fall back to tag, got %q", second.Name)
	}
}

func TestFetchChangelogCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0","name":"v1.0.0","body":"init","published_at":"2026-01-01T00:00:00Z","html_url":"https://github.com/a/b/releases/tag/v1.0.0","draft":false}]`))
	}))
	defer server.Close()

	t.Setenv("githubProxy", server.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	repo := "a/b"
	changelogCache.Delete(repo)

	for i := 0; i < 3; i++ {
		if _, err := FetchChangelog(repo); err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 upstream call due to cache, got %d", calls)
	}
	if os.Getenv("GITHUB_TOKEN") != "test-token" {
		t.Error("token env unexpectedly modified")
	}
}
