package image

import (
	"context"

	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type SaveRepoMapLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSaveRepoMapLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveRepoMapLogic {
	return &SaveRepoMapLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SaveRepoMapLogic) SaveRepoMap(req *types.SaveRepoMapReq) (resp *types.Resp, err error) {
	resp = &types.Resp{}
	repo, err := module.SaveRepoMapping(req.ImageName, req.Repo)
	if err != nil {
		l.Errorf("保存仓库映射失败: %v", err)
		resp.Code = 500
		resp.Msg = "保存失败: " + err.Error()
		resp.Data = map[string]interface{}{}
		return resp, nil
	}

	resp.Code = 200
	resp.Msg = "success"
	resp.Data = map[string]interface{}{
		"imageName": req.ImageName,
		"repo":      repo,
	}
	return resp, nil
}
