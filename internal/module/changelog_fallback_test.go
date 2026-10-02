package module

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchChangelogCommitsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/releases"):
			// 仓库存在但从未发布过 Releases
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/commits"):
			_, _ = w.Write([]byte(`[
				{
					"sha": "abcdef1234567890",
					"html_url": "https://github.com/a/b/commit/abcdef1234567890",
					"commit": {
						"message": "fix: 修复某个bug\n\n详细说明第二行",
						"author": {"date": "2026-09-01T12:00:00Z"}
					}
				}
			]`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("githubProxy", server.URL)
	changelogCache.Delete("a/b")

	data, err := FetchChangelog("a/b")
	if err != nil {
		t.Fatalf("FetchChangelog error: %v", err)
	}
	if data.Kind != "commits" {
		t.Errorf("kind = %q, want commits", data.Kind)
	}
	if len(data.Releases) != 1 {
		t.Fatalf("expected 1 commit entry, got %d", len(data.Releases))
	}
	entry := data.Releases[0]
	if entry.TagName != "abcdef1" || !strings.HasPrefix(entry.Name, "fix: 修复某个bug") {
		t.Errorf("entry = %+v", entry)
	}
}

func TestFetchChangelogBothMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("githubProxy", server.URL)
	changelogCache.Delete("ghost/repo")

	_, err := FetchChangelog("ghost/repo")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("want ErrRepoNotFound, got %v", err)
	}
}

func TestFetchChangelogCandidateWalking(t *testing.T) {
	// 第一个候选 404，第二个候选有 Releases，模拟逻辑层遍历
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "wrong/repo") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "right/repo") {
			_, _ = w.Write([]byte(`[{"tag_name":"v1.0","name":"v1.0","body":"init","published_at":"2026-01-01T00:00:00Z","html_url":"https://github.com/right/repo/releases/tag/v1.0","draft":false}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("githubProxy", server.URL)
	changelogCache.Delete("wrong/repo")
	changelogCache.Delete("right/repo")

	var got *ChangelogData
	var lastErr error
	for _, repo := range []string{"wrong/repo", "right/repo"} {
		data, err := FetchChangelog(repo)
		if err != nil {
			if errors.Is(err, ErrRepoNotFound) {
				lastErr = err
				continue
			}
			t.Fatalf("unexpected error: %v", err)
		}
		got = data
		break
	}
	if got == nil {
		t.Fatalf("no candidate matched, lastErr=%v", lastErr)
	}
	if got.Repo != "right/repo" || len(got.Releases) != 1 {
		t.Errorf("unexpected data: %+v", got)
	}
}
