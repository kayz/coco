# onboard 设计说明

## 1. 目标与范围

`onboard` 的目标是把 `relay / keeper / both` 三种模式的初始化统一为可引导、可校验、可自动化的流程。

本设计覆盖：
- 运行模式选择与最小必填配置
- AI provider/model 配置写入
- 长期服务化安装与启动
- 非交互自动化（CI/脚本）

本设计不覆盖：
- 平台连通性在线探测（如 API 余额、token 可用性）
- 复杂模板组合（多 provider 多模型策略编排）

## 2. 核心流程

`onboard` 按步骤执行：

1. `mode` 步骤
   - 选择模式：`relay | keeper | both`
2. `ai` 步骤
   - 选择 AI provider（内置或 custom）
   - 填写 API key、base_url、主/备模型
3. `mode-config` 步骤
   - 按模式提问必填项（条件题）
4. `service` 步骤（可跳过）
   - 安装并可选立即启动服务

## 3. 可扩展架构

`onboard` 采用声明式步骤模型，核心结构：

- `onboardStep`
  - `Name`
  - `Questions`
  - `Apply`
- `onboardQuestion`
  - `Key`（配置键）
  - `Prompt`
  - `Required`
  - `Default(state)`
  - `Validate(value, state)`
  - `Condition(state)`
- `onboardState`
  - 当前配置、模式、答案、预填值、交互输入器

扩展方式：
- 新增配置域：添加新的 `step` 或给现有步骤追加 question
- 条件化问题：使用 `Condition` 控制出现时机
- 新校验规则：新增 `validateXxx` 并挂接到 question
- 新 provider 模板：扩展 `providerTemplates`

该结构保证后续新增功能不会破坏既有流程顺序和自动化参数。

## 4. 配置写入策略

`onboard` 最终写入三类文件：

1. `.coco.yaml`
   - 模式、relay、keeper、平台凭据
2. `.coco/providers.yaml`
   - provider 列表（当前由向导生成单 provider）
3. `.coco/models.yaml`
   - 模型列表（主模型 + 可选备模型）

文件路径由可执行文件目录决定（与运行时一致）。

## 5. 关键默认值设计

- 默认模式：`relay`
- `both` 模式自动设本地链路：
  - `relay.server_url = ws://127.0.0.1:<keeper.port>/ws`
  - `relay.webhook_url = http://127.0.0.1:<keeper.port>/webhook`
- `both` 模式默认 `relay.token` 继承 `keeper.token`
- `relay.use_media_proxy`：
  - 官方云 relay 默认 `yes`
  - 自建 keeper / both 默认 `no`

## 6. 字段校验规则

内置校验包括：

- `mode` 必须是 `relay|keeper|both`
- `relay.platform` 必须是 `wecom|feishu|slack|wechat`
- `relay.server_url` 必须是 `ws/wss`
- `relay.webhook_url` 必须是 `http/https`
- `keeper.port` 必须是 `1..65535`
- `keeper.wecom_aes_key` 必须 43 字符
- `yes/no` 类型字段统一布尔解析

附加规则：
- `relay + wecom + 官方云 relay` 时，要求 `relay.wecom_*` 完整

## 7. 服务化设计

向导尾部可直接执行服务化：

- `service.Install(execPath, mode)`
- 可选 `service.Start(mode)`

模式独立服务标识：
- Darwin: `com.kayz.coco.<mode>`
- Linux: `coco-<mode>.service`

说明：
- Windows 当前不支持该服务管理实现，会自动提示并跳过。

## 8. 自动化能力（非交互）

`--non-interactive` + `--set key=value` 允许完全无交互初始化。

推荐用法：
- 预置密钥与模式，禁用服务提示：`--skip-service`
- 在部署管道中以环境变量模板生成参数

## 9. 与运行时一致性约束

为防止“向导写入但运行不生效”，`onboard` 的字段键直接对齐现有 `config` 与 `cmd` 读取路径：

- `cfg.Mode`
- `cfg.Relay.*`
- `cfg.Keeper.*`
- `cfg.Platforms.WeCom.*`（云 relay wecom 场景）

## 10. 后续演进建议

建议按以下方向扩展：

1. 增加 `post-check` 步骤
   - 本地端口占用检查
   - URL 可达性检查
2. 增加“配置概要确认”步骤
   - 最终写入前展示 diff
3. provider 模板分离到独立文件
   - 支持热更新和版本化
4. 引入迁移器
   - 兼容历史配置字段重命名
