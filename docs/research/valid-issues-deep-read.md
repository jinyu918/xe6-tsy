# xe6-tsy 主线 Issue/Proposal 深读

更新时间：2026-08-25（Asia/Shanghai）  
来源：GitHub 官方 REST API 的 Issue/PR 正文与评论；页面链接作为可复核入口。

## 研究口径

- 本次重点核对用户指定的 #81、#302、#213、#210、#204、#198、#269、#265、#194、#212、#186、#176、#181、#184、#174、#270、#274、#301、#200，以及已合并 PR #288、#290、#291、#293、#294、#296、#298。
- 政务、政策材料和 RAG/知识库方向不纳入产品主线事实。
- 本批条目中未发现 `invalid` 标签的 PR，也未发现 `no planed`/`no planned` 标签或标题命中；Project 字段、私有自动化状态和未公开评论无法由公开 API 证明，不能据此宣称不存在。
- Proposal、Task 和 open Issue 只表示候选输入；只有已明确合并的 PR 或已在当前仓库契约/实现中得到验证的内容，才可作为“当前 PRD 事实”。其余标为【候选】、【待确认】或【背景约束】。

## 条目结论总表

| 条目 | GitHub 状态 | 主线主题 | 当前 PRD 可用性 |
| --- | --- | --- | --- |
| [#81](https://github.com/1024XEngineer/xe6-tsy/issues/81) | open，无标签，Proposal | P0 产品场景与交互基线 | 【历史候选/冲突来源】可用于识别早期产品假设；与用户最新 P0 边界冲突的内容全部排除 |
| [#302](https://github.com/1024XEngineer/xe6-tsy/issues/302) | open，product，Proposal | Lingow 总体 PRD 草案 | 【候选】最新需求输入；不是已批准基线 |
| [#213](https://github.com/1024XEngineer/xe6-tsy/issues/213) | open，proposal | assistant/interpretation 模式编排 | 【候选】评论已冻结部分架构边界，仍需产品采纳 |
| [#210](https://github.com/1024XEngineer/xe6-tsy/issues/210) | closed，proposal | 单向/双向播报与投递 | 【候选】方案明确，但依赖消息投递产品决策 |
| [#204](https://github.com/1024XEngineer/xe6-tsy/issues/204) | open，4 条评论 | KWS 与 LLM 意图识别 | 评论形成方向性决策，仍非实现事实 |
| [#198](https://github.com/1024XEngineer/xe6-tsy/issues/198) | closed，proposal | 小智设备语音切换同传 | 【候选/设备专项】不可作为 Web P0 事实 |
| [#269](https://github.com/1024XEngineer/xe6-tsy/issues/269) | open，Task | 流式短语翻译与播放 | 【候选/P1】正文定义了可测规则，未说明已上线 |
| [#265](https://github.com/1024XEngineer/xe6-tsy/issues/265) | open，Task | ASR partial 展示、TTS 抢话 | 【候选】与 #269/#212 相关，开关及边界待确认 |
| [#194](https://github.com/1024XEngineer/xe6-tsy/issues/194) | closed，proposal | 收音和翻译速度 | 【背景约束】目标和方向可参考，未给出已交付证明 |
| [#212](https://github.com/1024XEngineer/xe6-tsy/issues/212) | closed，proposal | 播放打断深模块方案 | 【候选/工程方案】规则可进入验收，接口是否现行需核对代码 |
| [#186](https://github.com/1024XEngineer/xe6-tsy/issues/186) | closed，MiniSpec | 多语言闭环、目录和历史 UI | 【候选】验收清楚，但范围外明确排除 KWS/单向语音切换 |
| [#176](https://github.com/1024XEngineer/xe6-tsy/issues/176) | open，1 条评论 | 多渠道逐句自动投递 | 【候选】评论标记“已确认”，但尚未由合并实现证明 |
| [#181](https://github.com/1024XEngineer/xe6-tsy/issues/181) | closed，MiniSpec | participant 持久化所有权 | 【候选/架构约束】需与现行代码核对后采用 |
| [#184](https://github.com/1024XEngineer/xe6-tsy/issues/184) | closed，MiniSpec | 记录模块性能和契约收口 | 【候选/工程约束】未单独证明全部落地 |
| [#174](https://github.com/1024XEngineer/xe6-tsy/issues/174) | closed，MiniSpec | AI 异步链路内部鉴权 | 【风险事实】正文指出生产链路未接通；不能当成能力已存在 |
| [#270](https://github.com/1024XEngineer/xe6-tsy/issues/270) | open，proposal | ESP32 WebSocket 正式设备身份 | 【候选/设备专项】当前 Web PRD 非核心范围 |
| [#274](https://github.com/1024XEngineer/xe6-tsy/issues/274) | closed | session_id 一致性哈希多实例 | 【工程候选】实现边界明确，需以部署验收为准 |
| [#301](https://github.com/1024XEngineer/xe6-tsy/issues/301) | closed，MiniSpec | 双节点 40 会话长稳结果 | 【验证证据】仅 mock Provider/固定环境，不是生产 SLA |
| [#200](https://github.com/1024XEngineer/xe6-tsy/issues/200) | open，MiniSpec | 压测基线 | 【候选/发布门槛】计划，不是现成容量保证 |
| [#288](https://github.com/1024XEngineer/xe6-tsy/pull/288) | merged 2026-08-21 | README 与 dev runtime 对齐 | 【当前文档事实】只证明文档变更已合并 |
| [#290](https://github.com/1024XEngineer/xe6-tsy/pull/290) | merged 2026-08-21 | pipeline 测试强化 | 【当前测试事实】只改测试，不改生产行为 |
| [#291](https://github.com/1024XEngineer/xe6-tsy/pull/291) | merged 2026-08-21 | command gate 测试强化 | 【当前测试事实】只改测试，不改生产行为 |
| [#293](https://github.com/1024XEngineer/xe6-tsy/pull/293) | merged 2026-08-21 | WebRTC/Pion 测试强化 | 【当前测试事实】只改测试，不改生产行为 |
| [#294](https://github.com/1024XEngineer/xe6-tsy/pull/294) | merged 2026-08-21 | Silero VAD 测试强化 | 【当前测试事实】只改测试，不改生产行为 |
| [#296](https://github.com/1024XEngineer/xe6-tsy/pull/296) | merged 2026-08-21 | runtime 状态/命令反馈测试 | 【当前测试事实】只改测试，不改生产行为 |
| [#298](https://github.com/1024XEngineer/xe6-tsy/pull/298) | merged 2026-08-24 | Opus 短语连续播放 | 【实现事实】已合并；仍需关注评审发现和开关默认值 |

## 逐条深读

### #81：Proposal: 共识文档

正文提出的早期 P0 是“多人围绕同一台电脑轮流说话”，并要求会话内根据声音特征区分发言者、按首次出现顺序生成“发言者1/2/3”等匿名编号，同时由 ASR 在每轮自动识别源语言并决定翻译方向。它还将桌面浏览器、字幕卡片、历史记录、固定测试账号和原始音频尽力上传列入配套范围；长发言采用分段 TTS，P0 默认不展示 partial，但发言结束后展示完整原文/译文。

该 Issue 是重要的历史需求来源，但不是当前批准口径。用户随后明确覆盖了其中的关键决策：P0 目标用户是个人临时面对面交流，通常两人；不做会话内说话人识别、匿名发言者编号、声纹身份或现实身份绑定；不做每轮自动重新识别源语言；字幕不是必需交互；不提供登录和历史入口；不存在“切换会话”功能。当前 PRD 只保留 #81 对“轮流发言、两种语言、长句可能阻塞”的问题描述，并将说话人、每轮自动识别、字幕强制、历史和固定账号内容列为冲突/排除项。

### #302：Lingow 产品需求文档

正文是 2026-08-25 的 v1.2“评审草案”，覆盖账户、会话/语言配置、WebRTC 实时传译、assistant/interpretation 模式、打断、历史、用量、消息投递、设备和多实例等完整范围。它明确区分 P0、P1 和不做项，强调 `services/api`、`services/realtime-audio`、`packages/contracts` 的职责边界，以及 Start/End/语言配置/模式命令/FinalTurn/UsageFact 的幂等、版本和 generation 约束。

关键需求/决策：同一 VoiceSession 复用一条连接；模式切换不重连；普通 Turn 固定 mode/config 快照；FinalTurn 文本等事实不可变；partial/phrase 是临时事实；播放打断不回滚已提交事实；不纳入多人会议、政务/RAG、完整 Mobile UI、硬件制造、订单支付等。正文同时保留了“新 Web 默认 assistant、旧调用兼容 interpretation”“P1 流式短语”等未完全冻结内容。

冲突/限制：正文内部仍有“P0 展示 partial”与部分历史方案的冲突风险；它是 open Proposal，不能把其中的 P0 表述直接等同于已经批准的需求。可作为当前 PRD 的总体候选框架和冲突清单，不能作为无条件事实。

### #213：AI 对话助手模式编排

正文只交付 assistant 与 interpretation 的端到端模式切换，不实现英语口语训练；要求两个物理服务、一个 VoiceSession/Runtime/PeerConnection，realtime 保存 active mode 事实，API 负责授权、配置、审计和长期投影。支持 `start_mode`、`stop_mode`、`return_to_assistant`、`set_language_pair` 等白名单命令，命令必须有 `session_id/trace_id/command_id` 和幂等键。唤醒词后的命令音频独立于普通翻译 Turn；模式/配置和 generation 在 Turn 开始时固定。

评论进一步明确：缺省模式的旧调用按 `interpretation` 兼容，新客户端显式请求 `assistant`；FinalTurn 只属于同传，助手用独立 `AssistantReply`；不引入 `ActivitySession`；模式切换不执行 Stop/Start、不重连；未来模式只作为未注册扩展位。后续评论提出用 KWS + Command ASR + LLM + Registry/Validator/Executor 实现通用语义命令，但这是后续计划，不是已交付事实。

冲突：与 #302 中“新 Web 默认 assistant/旧调用兼容 interpretation”可以对齐；与 #198 的小智设备形态、#204 的 KWS 策略存在实现入口差异。是否进入本次 PRD 取决于用户是否把 assistant 作为范围；当前只能作为候选架构约束。

### #210：单/双向同传模式切换

方案以现有 `OutputRoute` 为唯一权威，不增加重复的模式状态：双向均 TTS；单向时一个方向 TTS、另一个方向关闭 TTS 并发送消息。两条语言方向仍都做 ASR、翻译、FinalTurn 保存；Turn 开始时固定路由快照，切换从下一 Turn 生效。开启单向前需存在已验证且启用的消息目标；部分渠道成功则不补播，全部失败才补播并在 CAS 条件下恢复双向。

前端建议提供双向/单向分段控件、方向交换、状态和失败回滚；语音命令切换明确暂不在本方案范围。

冲突：依赖 #176 的自动投递触发模型；若本期不做消息投递，单向模式不能进入 P0。该条目 closed 但正文是方案，除非产品确认单向播报，否则只能作为候选。

### #204：指令系统与意图识别

正文比较“固定句式 + KWS 门禁”和“所有 ASR final 统一交给 LLM”，结论建议状态驱动混合方案。4 条评论形成了更明确方向：前台采用唤醒词 + LLM，去掉固定完整句式；进入同传后可保留 KWS，普通同传不让大模型参与命令判断，只有检测到唤醒词才接收后续内容做意图识别。评论也存在“当前翻译链路本身 ASR->LLM->TTS，统一 prompt”这一不同实现观点，说明最终职责边界仍需冻结。

当前可用决策：保留单独唤醒词作为命令门禁、命令音频不能混入普通翻译 Turn、LLM 输出必须经过后端白名单/状态校验。是否入会后所有 ASR 都进 LLM、命令窗口是一句话还是多轮、完整动作集合均为【待确认】。

### #198：小智 AI 意图识别接入纯语音同传模式切换

closed Proposal，目标是小智硬件通过结构化 `enter_interpretation`/`exit_interpretation` 意图复用 Lingow Session、语言配置、ticket、WebRTC；同传中唤醒命令音频只交给小智，不生成翻译 Turn；首期固定 `zh-CN <-> en-US`，启动/退出有语音反馈，失败可回到 assistant，End 可后台幂等重试。

明确排除开放 Agent、多人与跨设备、动态语言/音色/速度、小智协议泄漏到核心 Pipeline。它是设备专项候选，不应作为桌面 Web P0 的产品事实；可作为设备模式和抢占行为的参考。

### #269：同声传译链路流式翻译

open Task 定义一个 VAD utterance 内的稳定短语提交：标点优先、无标点稳定 500ms 提交、VAD final flush；已提交文本不可撤回，空文本/纯标点/单字/语气词过滤。每个 utterance 最多 5 个未完成 TTS segment，达到阈值后取消未开始 TTS、保留字幕，下一个 utterance 恢复；命令/唤醒仍可强打断。Opus 是第一优先下行，PCM/DataChannel 后续再改造成 chunk 播放；播放期间保留 AEC/NS/AGC 并观察 VAD/ASR 以防回声误入。

冲突：与早期“VAD/普通人声立即打断 TTS”方案冲突；需区分普通翻译语音、唤醒/命令和显式停止。5 段阈值、500ms 和过滤规则可作为候选验收参数，但因为 Issue 仍 open，不能直接视为 P0 已确认。

### #265：优化服务功能

open Task 仅提出两点：捕获 ASR 中间结果用于流式展示，并在 VAD 开始/静音时维持 ASR 长连接；同时识别外部人声打断 TTS，防止 TTS 自身回声触发打断。

正文没有给出语言、时序、阈值、失败/重试或默认开关。它是 #269（流式翻译）和 #212（打断协调）的需求来源候选，不能单独支撑可验收 P0。

### #194：翻译收音和翻译速度增强

closed Proposal 识别两类问题：WebRTC 默认收音距离不足；句级链路“说完 -> ASR final -> 翻译 HTTP -> TTS”延迟较高。建议参考实时翻译模型，用 partial 预测稳定语义片段、final 修正、分段 TTS，并记录 ASR/翻译/TTS 各段耗时。

这是问题陈述和方向性建议，不是具体实现或性能承诺；收音增强还没有方案。可作为性能/可观测性背景，不应把“低延迟”或距离指标写入 PRD 验收，除非另行确认。

### #212：TTS 播放中打断方案

closed Proposal 设计一个 realtime 内部 `PlaybackInterrupter` 深模块，按钮、VAD、唤醒词为触发适配器。要求按 session/playback_id/source 精确匹配、幂等；先停 Opus、清空 PCM、标记 interrupted，再取消 TTS；主动打断不能使 Pipeline 进入 failed，FinalTurn/译文/已产出用量保留。DataChannel 命令须绑定 PeerConnection session，不信任客户端 session_id；前端立即静音，后端停止写入后下一次播放再恢复。

提出 VAD 60-100ms 连续语音确认、150ms 重复保护和 `REALTIME_VOICE_BARGE_IN` 默认关闭，且真实 AEC 通过设备 E2E 前保持关闭。`<100ms` 本地听感和 P95 `<300ms` 是方案验收目标，但未证明已是产品 SLA。与 #269 的“普通语音不打断、命令强打断”应合并为分层规则。

### #186：实时传译前后端闭环与多语言能力完善

closed MiniSpec 要求语言目录、ASR/TTS 能力交集、短语言码到 BCP-47 规范化、启动配置校验、Opus 正式下行和 PCM 诊断路径；同时补齐历史会话入口和跨会话 Turn 查看。验收包含中俄双向、语言一致性、`unsupported_language` 不出现、Opus/PCM 编码标识、耗时和错误可观测性。

明确范围外：唤醒词、关键词命令识别、单向/双向模式语音切换，以及历史/用量数据模型重构。可作为多语言和历史 UI 候选需求；不能用它证明这些能力已经完整上线。

### #176：Lingow 多渠道发送

正文描述现有服务端异步出站能力：客户端选择已落库 FinalTurn，调用 `/api/v1/outbound-messages`，服务端生成不可变快照，经 Outbox/Queue/Worker 投递 email/wechat；不会因 FinalTurn 落库自动推送，当前 Web 没有接入消息目标或创建接口。

1 条评论提出“逐句自动推送”并标记“状态：已确认”：每个 FinalTurn 按语言输出规则自动创建独立异步消息，一个渠道一个目标，消息同时包含原文和译文；TTS 与投递互不阻塞；幂等键为 `auto:final_turn:{turn_id}:{channel}:{destination_ref}`。这与正文的“当前不会自动推”不同，是最重要的未决冲突。除非产品明确采纳评论方案，否则当前 PRD 应按“客户端批量发送，非自动推送”作为现状，不把逐句自动推送当事实。

### #181：说话人及转译记录模块

closed MiniSpec 建议 participant 持久化由 API attribution worker 单独拥有；realtime 只产出 provider speaker evidence，不直接写 `voice_session_participants`。FinalTurn 初始 pending/provisional，带 provider_speaker_id，异步 worker 通过唯一映射最终转为 confirmed/corrected；没有有效 provider ID 时不创建 participant；归属服务不可用不阻塞 FinalTurn。

该方案与“前端/Realtime 直接编号或按声音区分发言者”相冲突。用户已明确：会话内说话人区分、匿名编号、跨会话声纹身份和现实身份绑定均不做。因此 `participant`、provider speaker evidence、pending/provisional/confirmed/corrected 归属链路都不能进入当前主线 PRD；若未来保留，只能作为明确排除的历史架构候选。是否实际已按 API-only 写入，需要代码验证后才能上升为实现事实。

### #184：记录模块其余优化

closed MiniSpec 提出 history `(session_id, created_at DESC, id DESC)` 索引、单 session 查询避免全量 account scope、worker 按 provider key 查询、`merged_into` 索引和 EXPLAIN 验证；并要求收口 `language_config_version` 必填性、纯异步 pending 模型、死的 SpeakerAttributionReader、APIError.details、DATA_DESIGN/OpenAPI pending 描述。

这些是性能和契约收口任务，不自动等于已完成。可作为研发/测试检查项；是否写入正式 PRD 取决于本次产品功能是否触及历史、说话人和接口。

### #174：AI 异步链路落地

closed MiniSpec 指出两个 AI PATCH 接口在生产因 `ContextSystemAuthorizer` 只认测试上下文而永远 403；没有内部服务凭据中间件和实际异步调用方，因此 #83 的稳定映射/归属修正“代码存在但能力未接通”。

这是重要风险事实：不能在 PRD 中写“AI 异步归属已可用”。如果本次 PRD依赖说话人异步修正，必须增加内部鉴权、任务投递、重试和观测作为研发依赖，或把归属修正降级为 pending。

### #270：小智 ESP32 WebSocket 正式设备身份

open Proposal 目标是把局域网固定 `DEVICE_WS_TOKEN`、内存 Session/语言配置的设备 WebSocket MVP 接入正式 Ed25519 provisioning、device token、API-owned Session、device-bound realtime ticket 和 transport-aware connection manager。重点规则包括设备 profile（16kHz Opus/WebSocket 与浏览器 WebRTC profile 区分）、设备语言配置路由、ticket/session/device 三方绑定、断线清理/重连不复用旧 Session、static/ticket 鉴权模式互斥且正式模式 fail closed。

它是设备接入与安全架构候选，不应进入桌面 Web P0；可作为设备权限、ticket 绑定、断线和幂等的参考。正文中“dev 已提供正式能力”是 Proposal 自述，仍需仓库代码/测试证实。

### #274：基于 session_id 的最小多实例会话分流

closed Issue 规定 Gateway 只处理 `/realtime/v1/sessions/{session_id}/...`，使用纯 session_id 一致性哈希；同一会话的 config/offer/ICE/Start/Stop/runtime/mode/fallback 请求必须固定同一节点，失败不得重试到其他节点。目标是固定节点集合的最小分流，不包含 API 多实例、动态发现、自动扩缩容、活动会话迁移或生产 TURN/UDP 方案。

可作为工程部署约束和测试范围；不能从该条目推导故障迁移或动态弹性能力。评论关联 PR #275，说明实现已交付方向，但本次用户指定的是 Issue #274，仍应以实际部署配置/测试作为最终事实。

### #301：固定双节点 40 会话长稳验证

closed MiniSpec 是 #200/#274 的验证记录：2026-08-24，1 API + 2 Realtime + Nginx，确定性 mock Provider，40 个并发 WebRTC 会话，30 分钟长稳；18,000 FinalTurn、54,000 UsageFact 均无丢失/重复，积压清零，连接资源回落，40 会话分布 27/13，Turn p95/p99 约 923/925ms。

这只能作为“在给定 Windows/本地拓扑和 mock 延迟下的验证证据”，不能作为生产容量、真实 Provider SLA、语言质量或跨地域网络承诺。PRD 若引用必须同时写明环境和 mock 限制。

### #200：实时转译首轮压力测试基线

open MiniSpec 要求以并发会话数 C + Turn 速率建模，k6 覆盖 API 控制面，Go/Pion 客户端按 20ms Opus 发送，默认关闭 WAV 落盘，fake Provider 隔离供应商，并采集 HTTP/WebRTC/Pipeline/队列/Go/DB/Redis 指标。验收要求 0.5C/C/1.3C/2C 阶梯、错误率/p95/p99/建连率/积压/资源、FinalTurn/UsageFact 无丢失重复和资源回落。

它是容量基线计划而非能力事实；#301 是一次具体验证。不得把 C、1.3C 或 2C 当作已支持并发，必须先定义环境规格和持续容量拐点。

## 已合并 PR 的实现/验证事实

### #288

2026-08-21 合并。仅将 README 对齐 `dev` runtime、控制面/媒体面、assistant/interpretation、delivery、Device SDK 和启动方式，并消除合并冲突。可作为仓库文档事实；不证明产品功能已实现。

### #290

2026-08-21 合并。仅补 pipeline P0/P1 测试：ASR/Turn/运行时认领、partial、播放生命周期/中断、assistant 依赖及用量/错误路径；未改生产逻辑。定向 mutation score：assistant 1.0、flow 0.933333、playback 0.885246。

### #291

2026-08-21 合并。仅补 command Gate/Registry 测试，包括清理副作用、Interrupt/Close feedback、命令 ID 256 上限和输入边界；未改生产行为。mutation score 1.0。

### #293

2026-08-21 合并。仅补 WebRTC memory/media/Pion factory 的输入、状态、ICE、PCM/DataChannel、RTP/SDP 和错误路径测试；报告目标文件总 mutation score 1.0。它是测试覆盖证据，不是新产品需求。

### #294

2026-08-21 合并。仅补 Silero VAD 阈值、推理输入/输出、状态迁移和资源解压安全测试；全量 mutation score 0.468750（仍有存活变异），未改生产行为。VAD 边界不能据此直接宣称生产准确率。

### #296

2026-08-21 合并。仅补 runtime manager、ModeCoordinator、command feedback 的状态和错误路径测试；未改生产代码。focused mutation score 分别为 keyed locker 0.8125、mode coordinator 0.5854、command feedback 0.6000，仍有存活/跳过变异。

### #298

2026-08-24 合并，实现流式 interpretation Phase 3：会话级短语播放调度器、独立 playback_id、顺序 Opus 下行、generation 防迟到音频、单 utterance 5 个未完成 TTS 段后字幕降级、下一 VAD utterance 恢复、AEC/NS/AGC/KWS 保持和 wake/command/stop 强打断。配置开关为 `REALTIME_PHRASE_SUBTITLES=enabled`、`REALTIME_PHRASE_PLAYBACK=enabled`、`REALTIME_TTS_DOWNLINK=opus`。

评论记录了三轮外部审查：初始 usage ID 冲突和 utterance 状态泄漏已修复；随后发现并修复首个 translation enqueue 的初始化竞态；最终审查无新增问题。Web 测试在审查环境因 vitest 不可执行而未运行，`go test`、格式和 diff 检查通过。该 PR 可作为“实现已合并”的事实，但开关默认值、生产灰度、真实音频质量和浏览器 E2E 仍需单独确认。

## 关键冲突与 PRD 处理建议

1. **说话人识别**：用户已明确会话内按声音区分、匿名编号、跨会话声纹身份和现实身份绑定全部不做。#181 及任何 #302 相关 participant/speaker attribution 描述均不得进入主线需求；FinalTurn 不应依赖说话人归属。
2. **语言识别与切换**：不要把“每轮重新识别源语言”写入当前需求事实。当前产品口径是会话开始前锁定语言对，并支持会话中切换；切换的生效边界（立即或下一 Turn）需要以现行 contracts/实现和产品确认统一。每轮只按当前生效的会话语言配置处理，不做任意源语言自动识别。
3. **partial/流式**：#265/#269/#298 支持流式 partial/短语，但 #186 的核心闭环仍可按 FinalTurn 验收。PRD 应拆分 P0 Final 与可配置 P1 phrase，明确 partial 是否展示；不能同时写“完全不展示 partial”和“P0 必须展示 partial”。
4. **打断**：#212 的按钮/VAD/KWS 统一 Interrupter 与 #269/#298 的普通人声不打断、命令强打断应合并成触发等级，而不是单一“人声即停止”。
5. **消息投递**：#176 正文描述客户端批量发送，评论提出逐句自动发送并称已确认；在产品负责人明确前，PRD 只能将批量出站作为现状，将自动逐句推送列为待确认。
6. **assistant 范围**：#213/#204/#198 形成 assistant/interpretation 同连接切换方向；若本次 PRD 是 Web 传译专项，assistant、KWS 和设备应列 Non-goal 或后续，不能从 Proposal 直接扩大 P0。
7. **容量**：#200 是测试计划，#301 是 mock 环境 40 会话结果，二者均不提供真实生产 SLA。PRD 应把容量阈值、浏览器/网络矩阵和 Provider 质量列为待确认。
8. **说话人异步修正**：#181 的 API-only pending 方案与 #174 的生产 403 风险同时成立，但本次用户边界已明确整个说话人区分功能不做。因此该链路应列 Non-goal，不应通过补内部鉴权把它重新纳入本期。

## 可作为当前 PRD 事实的最小集合

- 架构边界：`services/api` 管理账户/会话/语言/历史，`services/realtime-audio` 管理媒体和实时运行态，`packages/contracts` 是跨服务契约来源（由 #288 对齐文档、#302 草案重复说明）。
- 已合并实现：#298 的 Opus 短语播放、playback_id、generation 防迟到音频和 5 段字幕降级，但必须受 feature flag 和真实 E2E 验证约束。
- 已合并测试证据：#290/#291/#293/#294/#296 只说明对应模块测试覆盖增强，不能转化为用户功能承诺。
- 已验证但有条件：#301 的双节点 40 会话 mock 长稳结果，仅能作为本地/确定性 Provider 证据。
- 产品明确排除：跨会话声纹身份、现实身份绑定、政务/RAG、多人会议和未批准的设备/消息扩展。

其余需求（assistant 默认入口、会话中语言切换生效时点、partial 是否展示、KWS 命令窗口、自动逐句投递、设备正式鉴权、单向播报）均应在正式 PRD 中保留来源并标记【候选】或【待确认】，不能默认为已批准事实。
