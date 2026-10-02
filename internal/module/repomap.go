package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ref "github.com/distribution/reference"
	"github.com/zeromicro/go-zero/core/logx"
)

// 用户手动指定的镜像 -> GitHub 仓库映射，持久化在数据目录，
// 供「自动识别不到/识别错误」时人工纠正，保存后立即生效。
var repoMapFile = func() string {
	if f := os.Getenv("REPO_MAP_FILE"); f != "" {
		return f
	}
	return "/data/repo_map.json"
}()

var bareRepoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// NormalizeRepoInput 把用户输入的仓库地址归一化成 owner/repo。
// 支持完整 URL、github.com/owner/repo、owner/repo 三种写法，非法输入返回空串。
func NormalizeRepoInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if bareRepoRe.MatchString(input) && !strings.Contains(input, ".") {
		return input
	}
	return normalizeGitHubRepo(input)
}

// normalizeImageKey 把镜像名归一化成 domain/path（去掉 tag/digest）作为映射键
func normalizeImageKey(imageName string) string {
	named, err := ref.ParseDockerRef(strings.TrimSpace(imageName))
	if err != nil {
		return strings.TrimSpace(imageName)
	}
	return ref.Domain(named) + "/" + ref.Path(named)
}

func LoadRepoMap() map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(repoMapFile)
	if err != nil {
		return m
	}
	if err := json.Unmarshal(data, &m); err != nil {
		logx.Errorf("解析 repoMap 文件失败(%s): %v", repoMapFile, err)
	}
	return m
}

// SaveRepoMapping 保存一条镜像->仓库映射
func SaveRepoMapping(imageName, repo string) (string, error) {
	key := normalizeImageKey(imageName)
	value := NormalizeRepoInput(repo)
	if key == "" || value == "" {
		return "", fmt.Errorf("镜像名或仓库地址不合法: %q -> %q", imageName, repo)
	}

	m := LoadRepoMap()
	m[key] = value

	if err := os.MkdirAll(filepath.Dir(repoMapFile), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(repoMapFile, data, 0o644); err != nil {
		return "", err
	}
	return value, nil
}

// UserRepoOverride 查询用户手动指定的仓库，未配置返回空串
func UserRepoOverride(imageName string) string {
	key := normalizeImageKey(imageName)
	if repo, ok := LoadRepoMap()[key]; ok {
		return repo
	}
	// 兼容不带 registry 前缀的裸名再查一次（如保存时带 docker.io 前缀的写法差异）
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 && parts[0] == "docker.io" {
		if repo, ok := LoadRepoMap()[parts[1]]; ok {
			return repo
		}
	}
	return ""
}
