package module

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranslateBatchWithMockAI(t *testing.T) {
	dir := t.TempDir()
	aiCacheFile = filepath.Join(dir, "ai_cache.json")
	aiCache = syncMapReset()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = readJSON(r, &req)
		if req.Model != "test-model" {
			t.Errorf("model = %q", req.Model)
		}

		resp := `{"choices":[{"message":{"content":"<<<TAG v1.0.0>>>\n修复了若干问题。\n<<<TAG v0.9.0>>>\n首个版本。"}}]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_BASE_URL", server.URL)
	t.Setenv("AI_MODEL", "test-model")

	items := []ReleaseInfo{
		{TagName: "v1.0.0", Body: "fix bugs"},
		{TagName: "v0.9.0", Body: "first release"},
	}

	translated, err := TranslateBatch("a/b", items)
	if err != nil {
		t.Fatalf("TranslateBatch error: %v", err)
	}
	if translated["v1.0.0"] != "修复了若干问题。" || translated["v0.9.0"] != "首个版本。" {
		t.Errorf("translated = %v", translated)
	}

	// 第二次应全部命中缓存，不再调用 AI
	if _, err := TranslateBatch("a/b", items); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 AI call (cache), got %d", calls)
	}
}

func TestTranslateBatchAIFailureFallsBack(t *testing.T) {
	aiCacheFile = filepath.Join(t.TempDir(), "ai_cache.json")
	aiCache = syncMapReset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer server.Close()

	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_BASE_URL", server.URL)

	_, err := TranslateBatch("a/b", []ReleaseInfo{{TagName: "v1", Body: "x"}})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestParseAITranslationsIgnoresUnknownTags(t *testing.T) {
	items := []ReleaseInfo{{TagName: "v1.0.0", Body: "x"}}
	got := parseAITranslations("<<<TAG v1.0.0>>>\n中文内容\n<<<TAG v9.9.9>>>\n编造的", items)
	if len(got) != 1 || got["v1.0.0"] != "中文内容" {
		t.Errorf("got %v", got)
	}
}
