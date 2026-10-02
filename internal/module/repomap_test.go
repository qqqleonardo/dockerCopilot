package module

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoMapSaveAndOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REPO_MAP_FILE", filepath.Join(dir, "repo_map.json"))
	// 重置包内缓存的路径变量
	setRepoMapFileForTest(filepath.Join(dir, "repo_map.json"))

	if got := UserRepoOverride("eceasy/cli-proxy-api:latest"); got != "" {
		t.Fatalf("expected empty override before save, got %q", got)
	}

	saved, err := SaveRepoMapping("eceasy/cli-proxy-api:latest", "https://github.com/router-for-me/CLIProxyAPI")
	if err != nil {
		t.Fatalf("SaveRepoMapping error: %v", err)
	}
	if saved != "router-for-me/CLIProxyAPI" {
		t.Errorf("saved repo = %q", saved)
	}

	if got := UserRepoOverride("eceasy/cli-proxy-api"); got != "router-for-me/CLIProxyAPI" {
		t.Errorf("override after save = %q", got)
	}

	// 覆盖更新
	if _, err := SaveRepoMapping("eceasy/cli-proxy-api", "owner/other"); err != nil {
		t.Fatalf("overwrite error: %v", err)
	}
	if got := UserRepoOverride("docker.io/eceasy/cli-proxy-api:latest"); got != "owner/other" {
		t.Errorf("after overwrite = %q", got)
	}

	// 非法输入
	if _, err := SaveRepoMapping("x/y", "not a repo!!"); err == nil {
		t.Error("expected error for invalid repo")
	}
}

func TestNormalizeRepoInput(t *testing.T) {
	cases := map[string]string{
		"router-for-me/CLIProxyAPI":                "router-for-me/CLIProxyAPI",
		"https://github.com/router-for-me/CLIProxyAPI": "router-for-me/CLIProxyAPI",
		"github.com/a/b":                           "a/b",
		"  a/b  ":                                  "a/b",
		"https://github.com/a/b/releases/tag/v1":   "a/b",
		"justastring":                              "",
		"https://gitlab.com/a/b":                   "",
		"":                                         "",
	}
	for in, want := range cases {
		if got := NormalizeRepoInput(in); got != want {
			t.Errorf("NormalizeRepoInput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanReleaseBody(t *testing.T) {
	body := "下载 / Download based on your OS:\n\n<table>\n<tr><td><a href=\"x\"><img alt=\"s\" src=\"badge.png\"></a></td></tr>\n</table>\n\n---\n\nWhat's Changed:\n- **fix**: something &amp; another\n\nFull Changelog: https://github.com/a/b/compare/v1...v2"

	got := cleanReleaseBody(body)

	if strings.Contains(got, "<table") || strings.Contains(got, "<img") || strings.Contains(got, "href") {
		t.Errorf("HTML not stripped:\n%s", got)
	}
	if strings.Contains(got, "&amp;") {
		t.Errorf("entity not unescaped:\n%s", got)
	}
	if !strings.Contains(got, "What's Changed") || !strings.Contains(got, "**fix**: something & another") {
		t.Errorf("markdown content lost:\n%s", got)
	}
	if !strings.Contains(got, "下载链接表格已省略") {
		t.Errorf("table hint missing:\n%s", got)
	}
}

func TestCleanReleaseBodyPlainMarkdownUntouched(t *testing.T) {
	body := "## 更新内容\n- 修复 bug\n- 新增功能"
	got := cleanReleaseBody(body)
	if got != body {
		t.Errorf("plain markdown changed:\n%s", got)
	}
}

func setRepoMapFileForTest(path string) {
	repoMapFile = path
}
