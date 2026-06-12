// Package web embeds the Market Intel SPA build output (web/dist).
//
// dist/index.html 是进 git 的占位页；make web 用真实构建产物覆盖（约定不提交，
// 还原：git checkout web/dist/index.html）。Go 侧不感知占位与否 —— embed 的
// 就是编译时 dist/ 的内容。
package web

import "embed"

// DistFS 包含 "dist/" 前缀下的 SPA 产物（消费方用 fs.Sub 去前缀）。
//
//go:embed all:dist
var DistFS embed.FS
