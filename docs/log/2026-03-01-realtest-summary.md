# 2026-03-01 企业微信真人测试总结

## 总体结论

- 真人测试：基本通过
- 主要问题集中在：
  - relay 启动参数体验
  - 个别模型稳定性与自动治理

## 本次已修复

1. relay 启动简化（CLI）
   - 支持 `coco relay wecom`（位置参数平台）
   - wecom 场景在未提供 `user_id` 时自动生成本地 fallback user_id
   - 保留冲突保护：`--platform` 与位置参数不一致时直接报错
2. 本地默认配置补齐（`.coco.yaml`）
   - 已写入 `relay.platform`、`relay.user_id`、`relay.token`
   - 可直接 `coco relay` 启动

## 日志发现（待后续优化）

1. 模型侧失败样例
   - kimi 官方端点出现 404（`url.not_found`）
   - kimi/deepseek 在工具调用链路出现 `reasoning_content` 相关错误
   - 部分工具名校验失败（`function.name` pattern 不匹配）
2. 连接稳定性
   - 观测到 relay 侧间歇性 `unexpected EOF` 后自动重连
   - 当前具备自恢复，但建议后续做 keepalive/代理链路稳定性排查

## 后续动作建议

1. 增加模型健康分与自动下线（cooldown + 熔断 + 半开恢复）
2. 增加 `coco doctor models`/`coco models bench` 诊断命令
3. 为 Phase 2~7 建立固定回归套件（单测 + 集成 + 真人抽测）

---

## 追加调试记录（2026-03-01，onboard 安装流程）

### 现象

- 在 `C:\coco` 空目录执行 `coco onboard`。
- 交互完成到 `Step 1/6 [phase1-bootstrap]` 时，报错退出：
  - `phase1-bootstrap step failed: cloud relay with wecom requires relay.wecom_* fields`
- 触发条件：
  - `mode=relay`
  - `relay.platform=wecom`
  - `relay.server_url=wss://keeper.kayz.com/ws`（云 relay 默认地址）
  - 未填写 `relay.wecom_*` 凭据

### 影响

- 个人工具首次安装在“云 relay + wecom”默认路径上被阻断。
- 用户体验上属于“分支过多且强制输入过多参数”。

### 当前决策

- `onboard` 不再包含 keeper 连接测试与 heartbeat 上传（已移除该阶段）。
- keeper 侧配置改为手工处理，避免安装向导复杂化。
- 对于 `cloud relay + wecom` 的凭据强制策略，暂不继续改动，等待产品侧决策后再收敛。

### 待决策问题

1. `cloud relay + wecom` 是否允许“先完成安装，后补 relay.wecom_*”？
2. 若允许延后，默认降级策略是：
   - A) 改为本地 relay/keeper 地址模板
   - B) 保留云地址但仅给告警不阻断
   - C) 向导直接切换到非 wecom 平台模板（不推荐）
3. keeper 的“简单巡检/日程提醒”能力是否后续做最小独立向导（与主 onboard 解耦）？
