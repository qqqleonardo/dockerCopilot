package utiles

import "os"

// updateRepoBase 程序自检更新/自更新的仓库来源，默认上游原作者。
// fork 用户设置 updateRepo=<owner>/<repo> 后，版本检查与"更新程序"都指向自己的 fork，
// 避免被上游新版覆盖掉自定义功能。
func updateRepoBase() string {
	if repo := os.Getenv("updateRepo"); repo != "" {
		return repo
	}
	return "onlyLTY/dockerCopilot"
}
