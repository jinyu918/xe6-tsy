# xe6-tsy GitHub 产品事实研究

## 研究范围与方法

- 研究对象：`1024XEngineer/xe6-tsy` 公共 GitHub 仓库的 `main` 分支、README、公开 Issues 和 Pull Requests。
- 检索时间：2026-08-25（Asia/Shanghai）；仓库 API 返回的最近更新时间为 2026-08-21。
- API 版本快照（2026-08-25）：`main` 指向 [`5e3817982ba343544ea00844478d38b019b63935`](https://api.github.com/repos/1024XEngineer/xe6-tsy/branches/main)，`dev` 指向 [`2f9737ec1d76eeb070989b06e959ca7c82311f42`](https://api.github.com/repos/1024XEngineer/xe6-tsy/branches/dev)。README 与 Issue/PR 页面链接默认指向 `main`；涉及开发分支行为时单独标注 `dev`。
- 证据优先级：README 与 GitHub Issue/PR 正文、标签、状态和合并时间；Issue/PR 正文中的“建议”不视为已交付事实，除非对应 PR 已合并或 README 明确说明。
- 排除规则：
  1. 标题或正文明确涉及政务、基层政务窗口、政策/材料引用、机构工作人员等政务场景；
  2. 标题或正文明确涉及 RAG/知识库检索；
  3. 标记为 `no planed`/`no planned` 的 Issue；
  4. 标记为 `invalid` 的 PR。

GitHub 的公开 Issue 搜索未返回精确字符串 `no planed` 或 `no planned`，仓库标签列表也没有这两个标签（可复核 [labels API](https://api.github.com/repos/1024XEngineer/xe6-tsy/labels)）。因此本次没有因该条件命中而新增排除项；若项目使用评论、项目看板或非公开标签表达该状态，当前公开 API 无法证明，列为【不确定】。

## 仓库基线

- [README](https://github.com/1024XEngineer/xe6-tsy/blob/main/README.md) 将 Lingow 定义为面向 Web、移动端和硬件设备的 AI 语音助手与面对面双语传译系统。
- README 明确两种后端权威模式：`assistant` 与 `interpretation`，共用 WebRTC 会话；API 控制账户、会话、语言配置、记录、用量和投递，`realtime-audio` 负责 WebRTC、VAD、ASR、翻译、TTS、命令和运行态。
- README 当前边界明确：Web 是主要可运行联调入口；Mobile 只有控制面核心；Device SDK 不包含量产硬件实现；不提供管理后台、订单、支付、发票、多人会议同传或硬件制造能力。

## 纳入的产品/工程事实

### 当前产品决策与主链路

| 编号 | 标题 | 状态/标签 | 证据摘要 |
| --- | --- | --- | --- |
| [Issue #81](https://github.com/1024XEngineer/xe6-tsy/issues/81) | Proposal: 共识文档 | OPEN / 无标签 | 早期共识草案，提出单机多人、会话内匿名说话人编号、每轮自动源语言识别、字幕和历史；与用户最新确认的 P0（通常两人、不做说话人识别、不做每轮自动源语言识别、不要求字幕、不做历史入口）存在直接冲突，不能作为当前需求事实。 |
| [Issue #302](https://github.com/1024XEngineer/xe6-tsy/issues/302) | Proposal: Lingow 产品需求文档 | OPEN / `product` | 评审草案把 assistant 与 interpretation、WebRTC 会话、实时状态、打断、输出模式、恢复、账户和历史记录纳入产品讨论；正文自身仍是草案，不等同于已冻结需求。 |
| [Issue #213](https://github.com/1024XEngineer/xe6-tsy/issues/213) | Proposal: AI 对话助手模式编排 | OPEN / `proposal`, `enhancement`, `documentation` | 助手是默认模式；通过唤醒词和受控命令在同一条实时连接上进入/退出同传；英语训练仅保留未来扩展位，不在本 Issue 实现。 |
| [Issue #210](https://github.com/1024XEngineer/xe6-tsy/issues/210) | Proposal: 单/双向同传模式切换方案 | CLOSED / `proposal`, `MiniSpec` | 以 `OutputRoute` 作为单/双向播报的唯一权威配置，双向保留两个语言方向，单向将反向输出转为消息投递。 |
| [Issue #204](https://github.com/1024XEngineer/xe6-tsy/issues/204) | Proposal: 指令系统与意图识别方案讨论 | OPEN | 讨论唤醒词门禁、固定命令和 LLM 意图识别的职责边界；强调高风险命令需要确定性门禁。 |
| [Issue #198](https://github.com/1024XEngineer/xe6-tsy/issues/198) | Proposal: 小智 AI 意图识别接入纯语音同传模式切换 | OPEN / `proposal`, `MiniSpec`, `product` | 设备先唤醒并返回结构化 enter/exit interpretation 意图，再由网关创建 Lingow Session、获取 ticket、建立 WebRTC；聚焦最小模式切换闭环。 |

### 实时翻译、状态与播放

| 编号 | 标题 | 状态/标签 | 证据摘要 |
| --- | --- | --- | --- |
| [Issue #269](https://github.com/1024XEngineer/xe6-tsy/issues/269) | Task：同声传译链路流式翻译 | OPEN | 一个 VAD 轮次可产生多个稳定短语；标点、500ms 稳定时间或 VAD final 触发提交；已提交文本不可撤回；唤醒/命令优先级高于普通连续翻译。 |
| [Issue #265](https://github.com/1024XEngineer/xe6-tsy/issues/265) | Task：优化服务功能 | OPEN | 明确当前 ASR 忽略 partial，只使用 final；提出展示 partial 和为增量翻译保留长连接；要求外部人声打断 TTS 且自声不触发打断。 |
| [Issue #194](https://github.com/1024XEngineer/xe6-tsy/issues/194) | Proposal: 翻译收音和翻译速度增强 | CLOSED / `proposal`, `MiniSpec` | 记录非流式链路的延迟问题，提出 partial 草稿、稳定片段、final 修正、分段 TTS 和分段耗时记录；收音距离问题仍需真实环境验证。 |
| [Issue #212](https://github.com/1024XEngineer/xe6-tsy/issues/212) | Proposal: TTS 播放中打断方案 | CLOSED / `proposal`, `MiniSpec` | 建议统一 `PlaybackInterrupter`：按 `playback_id` 精确停止、先停输出再取消 Provider、重复调用幂等；打断不使 Pipeline 失败，已产生的 Final Turn/用量保留。 |
| [PR #298](https://github.com/1024XEngineer/xe6-tsy/pull/298) | feat: continuous Opus phrase playback | MERGED (2026-08-24) | 为每个短语建立独立 `playback_id`，按顺序投递 Opus，使用 generation 防止迟到音频；单轮未完成片段超过 5 个时降低排队 TTS。 |
| [PR #299](https://github.com/1024XEngineer/xe6-tsy/pull/299) | feat: streaming voice status and phrase translation | OPEN | Web 状态胶囊、波形和实时 transcript；传递带 `text`/`stash`/Provider language metadata 的 ASR partial，并锁定同传源语言。 |
| [PR #300](https://github.com/1024XEngineer/xe6-tsy/pull/300) | feat: 支持单向传译语音指令 | OPEN | 扩展 `output_mode` 与 `expected_version`；API 派生 TTS/投递路由；当前 Turn 使用旧快照，新配置下一 Turn 生效；配置失败不切换模式。 |

### 账户、记录、设备与部署

| 编号 | 标题 | 状态/标签 | 证据摘要 |
| --- | --- | --- | --- |
| [Issue #186](https://github.com/1024XEngineer/xe6-tsy/issues/186) | Task: 实时传译前后端闭环与多语言能力完善 | CLOSED / `MiniSpec` | 指出语言目录、ASR 语言码与 BCP-47 配置、历史入口、会话切换、用量和实时状态必须形成前端可完成的闭环。 |
| [Issue #176](https://github.com/1024XEngineer/xe6-tsy/issues/176) | Proposal: Lingow 多渠道发送设计说明 | OPEN | 服务端已有异步 Email/企业微信投递骨架，但由客户端选择已定稿 Final Turn 后批量发送；partial 和播放状态不自动推送。 |
| [Issue #181](https://github.com/1024XEngineer/xe6-tsy/issues/181) | Task: 优化说话人及转译记录模块 | CLOSED / `MiniSpec` | participant 持久化由 API 异步归属 worker 负责；realtime 只提供 speaker evidence；Final Turn 初始可保持 pending。 |
| [Issue #184](https://github.com/1024XEngineer/xe6-tsy/issues/184) | Task: 说话人及转译记录模块其余优化任务 | CLOSED / `MiniSpec` | 关注历史查询索引、异步 pending 模型、`language_config_version` 契约、错误详情和 speaker pending/confirmed 语义收口。 |
| [Issue #174](https://github.com/1024XEngineer/xe6-tsy/issues/174) | Task: AI 异步链路落地 | CLOSED / `MiniSpec` | 指出生产环境内部服务凭据和 AI 异步调用方缺失；相关 PATCH 不能仅凭测试上下文授权。 |
| [Issue #270](https://github.com/1024XEngineer/xe6-tsy/issues/270) | Proposal: 小智 ESP32 WebSocket 接入正式设备身份与 Session 控制面 | OPEN / `proposal` | 局域网 MVP 使用固定 token、临时 Session 和内存语言配置；正式方向是 Ed25519 设备身份、设备 Session 所有权和 realtime ticket。 |
| [Issue #274](https://github.com/1024XEngineer/xe6-tsy/issues/274) | 基于 session_id 的最小多实例会话分流 | CLOSED | Gateway 对 `session_id` 一致性哈希，使同一会话的 Offer/ICE/Start/Stop/runtime/mode 保持在同一 realtime 节点；不覆盖 API 多实例。 |
| [Issue #301](https://github.com/1024XEngineer/xe6-tsy/issues/301) | 固定双节点 40 会话长稳验证结果 | CLOSED / `MiniSpec` | 在 1 API + 2 realtime + 固定哈希网关、确定性 mock Provider 下验证 40 个同时在线 WebRTC 会话、FinalTurn/UsageFact 完整性和积压清空；结果不能直接推导生产 SLA。 |
| [Issue #200](https://github.com/1024XEngineer/xe6-tsy/issues/200) | 建立实时转译首轮压力测试基线 | OPEN / `MiniSpec` | 建议用 k6 + Go/Pion Opus 客户端、fake Provider，覆盖控制面、WebRTC、Pipeline、队列和存储指标；默认关闭调试 WAV 落盘。 |

### 已合并的质量和契约证据

| 编号 | 标题 | 状态/标签 | 证据摘要 |
| --- | --- | --- | --- |
| [PR #296](https://github.com/1024XEngineer/xe6-tsy/pull/296) | test(runtime): harden P0/P1 state, mode, and feedback coverage | MERGED (2026-08-21) | 覆盖 runtime 状态机、模式协调器、非法 Turn、命令反馈超时/关闭/TTS 失败/用量发布失败。 |
| [PR #294](https://github.com/1024XEngineer/xe6-tsy/pull/294) | test(silero): strengthen P0/P1 VAD mutation coverage | MERGED (2026-08-21) | 覆盖 VAD 阈值归一化、非法阈值、输入窗口和 ONNX 资源失败。 |
| [PR #293](https://github.com/1024XEngineer/xe6-tsy/pull/293) | test(webrtc): harden P0/P1 coverage for memory, media, and Pion factory | MERGED (2026-08-21) | 覆盖连接状态门、依赖校验、版本/过期状态、ICE 字段比较、媒体采样率和 PCM 边界。 |
| [PR #291](https://github.com/1024XEngineer/xe6-tsy/pull/291) | test(command): strengthen P0/P1 coverage for realtime command gate | MERGED (2026-08-21) | 覆盖 Gate 依赖、非法请求、关闭状态、定时器/ASR stream/context 清理和反馈中断。 |
| [PR #290](https://github.com/1024XEngineer/xe6-tsy/pull/290) | test(pipeline): strengthen P0/P1 mutation coverage | MERGED (2026-08-21) | 覆盖 ASR/Turn/运行时认领、partial、播放生命周期、中断、fallback 和 usage 恢复。 |
| [PR #288](https://github.com/1024XEngineer/xe6-tsy/pull/288) | docs(readme): align documentation with current dev runtime | MERGED (2026-08-21) | README 与 dev 运行时、模式和边界重新对齐，是当前仓库能力基线的直接来源。 |

## 排除项

### 政务、政策材料和 RAG

以下公开 Issue 属于早期政务/政策材料/知识库方向，不进入 Lingow 当前 PRD：

- [Issue #2](https://github.com/1024XEngineer/xe6-tsy/issues/2)、[#3](https://github.com/1024XEngineer/xe6-tsy/issues/3)、[#4](https://github.com/1024XEngineer/xe6-tsy/issues/4)、[#7](https://github.com/1024XEngineer/xe6-tsy/issues/7)、[#9](https://github.com/1024XEngineer/xe6-tsy/issues/9)：基层政务窗口或政务场景。
- [Issue #12](https://github.com/1024XEngineer/xe6-tsy/issues/12)：本地政策知识库配置，属于明确的知识库/RAG 方向。
- [Issue #20](https://github.com/1024XEngineer/xe6-tsy/issues/20)、[#33](https://github.com/1024XEngineer/xe6-tsy/issues/33)、[#61](https://github.com/1024XEngineer/xe6-tsy/issues/61)：政策辅助、政策与材料引用或政务领域插件。
- [Issue #23](https://github.com/1024XEngineer/xe6-tsy/issues/23)、[#24](https://github.com/1024XEngineer/xe6-tsy/issues/24)、[#38](https://github.com/1024XEngineer/xe6-tsy/issues/38)：基层政务窗口试点及其部署/隐私边界。

GitHub 搜索 `RAG` 未发现以 RAG 为主标题的现代 Issue/PR；搜索命中的早期政务/政策材料条目按上表排除。单纯提到“知识补充助手”但未明确 RAG 的 [Issue #190](https://github.com/1024XEngineer/xe6-tsy/issues/190) 也不作为当前 PRD 证据，避免将候选亮点误读为已确认范围。

### `invalid` PR

以下 PR 带有 GitHub `invalid` 标签，全部排除，不论其是否曾合并：[#27](https://github.com/1024XEngineer/xe6-tsy/pull/27)、[#28](https://github.com/1024XEngineer/xe6-tsy/pull/28)、[#29](https://github.com/1024XEngineer/xe6-tsy/pull/29)、[#44](https://github.com/1024XEngineer/xe6-tsy/pull/44)、[#45](https://github.com/1024XEngineer/xe6-tsy/pull/45)、[#46](https://github.com/1024XEngineer/xe6-tsy/pull/46)、[#52](https://github.com/1024XEngineer/xe6-tsy/pull/52)、[#53](https://github.com/1024XEngineer/xe6-tsy/pull/53)、[#54](https://github.com/1024XEngineer/xe6-tsy/pull/54)、[#55](https://github.com/1024XEngineer/xe6-tsy/pull/55)、[#56](https://github.com/1024XEngineer/xe6-tsy/pull/56)、[#57](https://github.com/1024XEngineer/xe6-tsy/pull/57)、[#58](https://github.com/1024XEngineer/xe6-tsy/pull/58)、[#59](https://github.com/1024XEngineer/xe6-tsy/pull/59)。

## 不确定性与使用边界

1. Issue 的 OPEN/CLOSED 状态表示 GitHub 工作项状态，不等于功能已上线；只有 README 或 MERGED PR 能作为较强实现证据。
2. OPEN PR（如 #299、#300）是候选变更，不能当作当前 `main` 已具备能力；正式 PRD 应将其标为候选或待确认。
3. #301 的 40 会话结果基于确定性 mock Provider、固定双节点和特定基线，不能直接转写为生产并发/SLA。
4. `no planed`/`no planned` 未在公开标签或搜索结果中出现；若该标记存在于评论、Project 状态或私有自动化元数据，需要项目维护者补充证据。
5. 本笔记不把早期政务/RAG 方向、`invalid` PR、单纯设计讨论或未合并 PR 自动转化为产品需求。

## 可复用结论

- 当前可作为 PRD 主线的事实是：Web 首发入口、assistant/interpretation 双模式、同一 WebRTC 会话、实时 ASR/翻译/TTS、Final Turn/UsageFact、播放打断、语言配置版本控制、账户/设备授权和会话级多实例分流。
- 当前最需要产品确认的边界是：首批语言、流式短语是否 P0、单向投递目标、多设备并发、性能/SLA 和设备正式接入。
- 评审时应把 OPEN Issue/PR 作为“候选输入”，把 README 和已合并 PR 作为当前实现基线，并保持政务/RAG/invalid/no-plan 条件隔离。
