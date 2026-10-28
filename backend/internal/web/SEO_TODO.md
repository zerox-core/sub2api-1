# SEO noindex 半成品（未提交）

`backend/internal/web/seo.go` 与 `seo_test.go` 在 2026-10-28 的提交整理中被**排除**，
原因是它们属于未完成的改动，`go test -tags embed ./internal/web/` 会失败：

- `applyRobotsNoIndexHeader` 从未被任何地方调用，HTML 响应不带 `X-Robots-Tag`
  → `TestLegacyServeEmbeddedFrontendMarksNoIndex` 失败
- `ServeRobotsTxt` 从未注册到路由，`/robots.txt` 仍被 SPA 兜底吞掉
  → `TestRobotsTxtIsNotShadowedBySPA` 失败

要启用这个功能，还差两步接线：

1. 在 `embed_on.go` 的 HTML 响应路径（含 settings 注入与 html_cache 分支）调用
   `applyRobotsNoIndexHeader(c.Writer.Header())`；静态资源不调用。
2. 在注册前端路由处挂上 `router.GET("/robots.txt", ServeRobotsTxt)`，并确保
   `shouldBypassEmbeddedFrontend("/robots.txt")` 返回 true。

做完这两步，`go test -tags embed ./internal/web/` 应当全绿后再提交。