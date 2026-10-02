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

func TestSourceRepoCandidates(t *testing.T) {
	cases := []struct {
		name      string
		imageName string
		labels    map[string]string
		wantFirst string
		wantAll   []string
	}{
		{
			"label优先",
			"ghcr.io/owner/x:latest",
			map[string]string{"org.opencontainers.image.source": "https://github.com/real/repo"},
			"real/repo",
			[]string{"real/repo", "owner/x"},
		},
		{
			"ghcr无label",
			"ghcr.io/guardinary/web2gem:latest",
			nil,
			"guardinary/web2gem",
			[]string{"guardinary/web2gem"},
		},
		{
			"dockerhub作者名猜测",
			"whyour/qinglong:latest",
			nil,
			"whyour/qinglong",
			[]string{"whyour/qinglong"},
		},
		{
			"linuxserver命名规律",
			"linuxserver/qbittorrent:latest",
			nil,
			"linuxserver/docker-qbittorrent",
			[]string{"linuxserver/docker-qbittorrent", "linuxserver/qbittorrent"},
		},
		{
			"官方库映射",
			"nginx:latest",
			nil,
			"nginxinc/docker-nginx",
			[]string{"nginxinc/docker-nginx"},
		},
		{
			"官方库带namespace映射",
			"library/redis:latest",
			nil,
			"redis/redis",
			[]string{"redis/redis"},
		},
		{
			"映射表命中优先于猜测",
			"xhofe/alist:latest",
			nil,
			"AlistGo/alist",
			[]string{"AlistGo/alist", "xhofe/alist"},
		},
		{
			"hub官方单段名",
			"vaultwarden/server:latest",
			nil,
			"dani-garcia/vaultwarden",
			[]string{"dani-garcia/vaultwarden"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SourceRepoCandidates(c.imageName, c.labels)
			if len(got) == 0 || got[0] != c.wantFirst {
				t.Errorf("candidates = %v, want first %q", got, c.wantFirst)
				return
			}
			for _, want := range c.wantAll {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("candidates %v missing %q", got, want)
				}
			}
		})
	}
}
