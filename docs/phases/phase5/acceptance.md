# Phase 5 Acceptance - 安全与完善

- [ ] 高风险 shell 命令被阻断或要求确认
- [ ] 非白名单来源消息被拒绝
- [ ] 群聊未 mention 可按策略忽略
- [ ] `web_fetch` 的 SSRF 防护生效
- [ ] 配置修改后无需重启即可热生效
- [ ] `go test ./internal/security ./internal/config ./internal/tools ./internal/agent`
