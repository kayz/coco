# onboard 用户手册（7阶段）

## 1. 命令
```bash
coco onboard [flags]
```

常用参数：
- `--mode relay|keeper|both`
- `--workspace <path>`：人格/记忆文件写入目录（默认当前目录）
- `--non-interactive`
- `--set key=value`（可重复）
- `--skip-service`（跳过自启动设置）

## 2. 交互流程
`coco onboard` 会依次完成：
1. 运行模式
2. AI provider / apikey / 模型
3. relay/keeper 必填运行参数
4. SOUL/USER/IDENTITY/JD/HEARTBEAT/MEMORY/TOOLS 文件写入
5. Obsidian vault 绑定 + 索引写入
6. 工具目录导出 + 工具烟雾测试
7. 开机启动配置 + 完成提示

## 3. 关键输出文件
- 配置：`.coco.yaml`、`.coco/providers.yaml`、`.coco/models.yaml`
- 人格与协作：`SOUL.md`、`USER.md`、`IDENTITY.md`、`JD.md`、`HEARTBEAT.md`、`TOOLS.md`
- 记忆：`MEMORY.md` + `memory/*` 核心文件
- 测试报告：`.coco/onboard/tool-smoke-report.md`
- Obsidian 索引：默认写入 `<vault>/.coco/coco-index.md`

## 4. Keeper 说明
当前版本的 `onboard` 不再处理 keeper 连接测试和 heartbeat 上传。  
keeper 保持手工配置，避免初始化流程过长。

## 5. 非交互示例（both）
```bash
coco onboard ^
  --mode both ^
  --workspace C:\git\coco ^
  --non-interactive ^
  --set ai.provider=deepseek ^
  --set ai.api_key=YOUR_API_KEY ^
  --set ai.base_url=https://api.deepseek.com/v1 ^
  --set ai.model.primary=deepseek-chat ^
  --set keeper.port=8080 ^
  --set keeper.token=YOUR_KEEPER_TOKEN ^
  --set keeper.wecom_corp_id=wwxxxx ^
  --set keeper.wecom_agent_id=1000001 ^
  --set keeper.wecom_secret=xxxx ^
  --set keeper.wecom_token=xxxx ^
  --set keeper.wecom_aes_key=YOUR_43_CHAR_AES_KEY ^
  --set relay.platform=wecom ^
  --set relay.user_id=wecom-wwxxxx ^
  --set memory.enabled=yes ^
  --set memory.obsidian_vault=D:\ObsidianVault ^
  --set tools.test=yes ^
  --set autostart.enable=no
```

## 6. 新增 `--set` 键
- 人格文件：`persona.assistant_name`、`identity.role`、`soul.core_truths`、`soul.vibe`、`jd.scope`
- 心跳：`heartbeat.interval`、`heartbeat.notify`、`heartbeat.focus`
- Obsidian：`memory.enabled`、`memory.obsidian_vault`、`memory.create_vault`、`memory.index_path`
- 工具与启动：`tools.export`、`tools.test`、`autostart.enable`、`autostart.start_now`

## 7. 常见问题
1. `memory enabled but obsidian_vault is empty`：补充 `memory.obsidian_vault`。
2. Windows 自启动：onboard 会写入启动目录 `.bat`，若失败请检查当前用户权限和 `APPDATA`。
