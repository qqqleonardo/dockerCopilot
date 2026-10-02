package image

import (
	"context"
	"errors"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ChangelogLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChangelogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChangelogLogic {
	return &ChangelogLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ChangelogLogic) Changelog(req *types.ImageChangelogReq) (resp *types.Resp, err error) {
	resp = &types.Resp{}

	imageID, imageName, labels, err := l.resolveImage(req.Id)
	if err != nil {
		resp.Code = 500
		resp.Msg = "未找到对应的镜像或容器: " + err.Error()
		resp.Data = map[string]interface{}{}
		return resp, nil
	}

	candidates := module.SourceRepoCandidates(imageName, labels)
	currentVersion := labels["org.opencontainers.image.version"]

	if len(candidates) == 0 {
		resp.Code = 200
		resp.Msg = "该镜像无法确定 GitHub 源仓库，无法获取更新说明"
		resp.Data = map[string]interface{}{
			"repo":           "",
			"imageId":        imageID,
			"currentVersion": currentVersion,
			"releases":       []module.ReleaseInfo{},
		}
		return resp, nil
	}

	var lastErr error
	for _, repo := range candidates {
		data, fetchErr := module.FetchChangelog(repo)
		if fetchErr != nil {
			if errors.Is(fetchErr, module.ErrRepoNotFound) {
				continue // 猜测的候选仓库不存在，试下一个
			}
			lastErr = fetchErr // 限流等真实错误，直接报给用户
			break
		}
		resp.Code = 200
		resp.Msg = "success"
		resp.Data = map[string]interface{}{
			"repo":           data.Repo,
			"kind":           data.Kind,
			"imageId":        imageID,
			"currentVersion": currentVersion,
			"releases":       data.Releases,
		}
		return resp, nil
	}

	if lastErr != nil {
		l.Errorf("获取更新说明失败(候选: %v): %v", candidates, lastErr)
		resp.Code = 500
		resp.Msg = "获取更新说明失败: " + lastErr.Error()
	} else {
		resp.Code = 200
		resp.Msg = "未能确定该镜像的 GitHub 源仓库，无法获取更新说明"
	}
	resp.Data = map[string]interface{}{
		"repo":           "",
		"imageId":        imageID,
		"currentVersion": currentVersion,
		"releases":       []module.ReleaseInfo{},
	}
	return resp, nil
}

// resolveImage 支持传镜像 id 或容器 id，返回镜像 id、镜像名和镜像 labels
func (l *ChangelogLogic) resolveImage(id string) (string, string, map[string]string, error) {
	lookupID := strings.TrimPrefix(id, "sha256:")

	img, _, err := l.svcCtx.DockerClient.ImageInspectWithRaw(l.ctx, lookupID)
	if err == nil {
		return img.ID, firstRepoTag(img.RepoTags), configLabels(img.Config), nil
	}

	// 不是镜像 id，按容器 id 处理
	containerInspect, err := l.svcCtx.DockerClient.ContainerInspect(l.ctx, lookupID)
	if err != nil {
		return "", "", nil, errors.New("image or container not found")
	}

	imageID := strings.TrimPrefix(containerInspect.Image, "sha256:")
	img, _, err = l.svcCtx.DockerClient.ImageInspectWithRaw(l.ctx, imageID)
	if err != nil {
		logx.Errorf("通过容器获取镜像信息失败: %v", err)
		return imageID, containerInspect.Config.Image, map[string]string{}, nil
	}
	name := firstRepoTag(img.RepoTags)
	if name == "" {
		name = containerInspect.Config.Image
	}
	return img.ID, name, configLabels(img.Config), nil
}

func firstRepoTag(repoTags []string) string {
	if len(repoTags) > 0 {
		return repoTags[0]
	}
	return ""
}

func configLabels(config *container.Config) map[string]string {
	if config == nil {
		return map[string]string{}
	}
	return config.Labels
}
