package module

import "testing"

func TestGetSourceRepo(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"标准地址", map[string]string{"org.opencontainers.image.source": "https://github.com/linuxserver/docker-qbittorrent"}, "linuxserver/docker-qbittorrent"},
		{"带.git后缀", map[string]string{"org.opencontainers.image.source": "https://github.com/AAAAA/qBittorrent.git"}, "AAAAA/qBittorrent"},
		{"结尾斜杠", map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo/"}, "owner/repo"},
		{"tree子路径", map[string]string{"org.opencontainers.image.source": "https://github.com/owner/repo/tree/master"}, "owner/repo"},
		{"无协议", map[string]string{"org.opencontainers.image.source": "github.com/owner/repo"}, "owner/repo"},
		{"非github源", map[string]string{"org.opencontainers.image.source": "https://gitlab.com/owner/repo"}, ""},
		{"docker hub页面", map[string]string{"org.opencontainers.image.source": "https://hub.docker.com/r/library/redis"}, ""},
		{"空labels", nil, ""},
		{"无该label", map[string]string{"org.opencontainers.image.version": "1.0.0"}, ""},
		{"只有owner", map[string]string{"org.opencontainers.image.source": "https://github.com/owner"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetSourceRepo(c.labels); got != c.want {
				t.Errorf("GetSourceRepo(%v) = %q, want %q", c.labels, got, c.want)
			}
		})
	}
}
