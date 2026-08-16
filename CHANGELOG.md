# 更新日志

## v1.20.0 - 2026-08-17

- 设置页「网络与服务」新增网关 Web Search 配置：可开关网关侧外部搜索，并配置单次请求的最大搜索重放轮数。默认 10 轮，支持 1-100 轮；上游模型只返回 `web_search` 工具调用时，网关访问 Bing 获取结果并回填重放，已由上游完成的原生搜索不走此流程。
- 修复小时统计在午夜 `00` 点无法保存的问题。`StatsHourly.Hour` 明确使用 `0-23` 的业务主键，不再被 GORM 当作自增 ID 处理。

## v1.11.0 - 2026-08-13

### ✨ 新功能：一处配好所有上游请求

设置页多了一张「上游请求」卡片，点「配置」进去，能统一给发往上游的请求加请求头、改请求参数。

以前这两件事只能一个渠道一个渠道地设，渠道多了就得重复劳动。现在可以：

- 🌍 **全局设一次，所有渠道都生效** —— 比如统一伪装 User-Agent
- 🎯 **只对某些模型生效** —— 填 `gpt-4*` 这样的通配符，一条规则管一批模型
- 🔧 **个别渠道要特殊对待** —— 渠道自己的设置优先级最高，随时能盖掉全局的

四层优先级从低到高：客户端请求 → 全局 → 模型规则 → 渠道，越靠后越大。

### ⚠️ 升级注意：嵌套设置的覆盖方式变了

渠道「高级设置」里的「参数覆盖」，改动嵌套设置时的行为变了。

举个例子：某项设置里有「开关」和「额度」两个子项，你只想改额度。

- 以前：改完额度，**开关会被一起清掉**
- 现在：只改额度，**开关保持原样**

绝大多数情况下这是你想要的。但如果你之前正是**利用这个特性来清掉某些子项**，那这些渠道的配置需要调整一下。

### 📌 生效范围

对话、补全、向量这些日常请求都生效。图片生成、WebSocket 长连接和后台健康检查这次还没接上。

没配任何东西时，请求完全按原样发出，不受影响。

## v1.10.3 - 2026-08-12

- 再次优化首页排行榜「总计」行的 UI：取消左侧独立占位，直接将「总计」字样移到右侧数值的前方（如“总计：5.88亿”），使界面更加紧凑自然。

## v1.10.2 - 2026-08-12

- 优化首页排行榜「总计」行的 UI：复用列表项的栅格布局，留出左侧的勋章占位并对齐右侧字号与字重，修复了 v1.10.1 中总计行内边距缺失与上下错位的问题。

## v1.10.1 - 2026-08-12

- 首页排行榜新增「总计」行：位于「按渠道 / 按模型」标签与列表之间，按当前激活标签对全部条目汇总——「金额」显示总花费（如 `0.60$`）、「次数」显示总请求数、「Tokens」显示总 token 数（中文界面按万/亿，如 `4.50亿`），「按渠道」与「按模型」两个维度均生效；列表为空时自动隐藏。

## v1.10.0 - 2026-08-12

- 修复 Responses 协议渠道（如 opencode 对接的 DeepSeek v4 flash 兼容端点）调用工具时只发 `response.output_item.added` / `response.function_call_arguments.delta`、缺发 `response.function_call_arguments.done` / `response.output_item.done`，导致依赖 `output_item.done` 收集工具调用的客户端（如 Hermes 的 responses 模式）丢掉工具调用、回合提前结束（"请求一次就停"）的问题。Responses 同协议透传此前把上游事件原样转发，事件缺失也随之透传；现在透传流按 SSE 块逐事件检查，`response.completed` / `failed` / `incomplete` / `error` 到达时若仍有未关闭的 function_call，会在终态事件前自动补齐这两个 done 事件（含全量 arguments 与 `status: completed` 的完整 item），客户端可继续执行工具。上游已发 done 事件的完整流（如基元律动）逐字节原样透传、不受影响；标准转换链（Chat 客户端）本就依赖 delta + completed，本次也补了链路验证测试。
- Chat 协议请求（`/v1/chat/completions`）打 Responses 协议渠道的转换补齐两处跨格式丢失：`max_tokens` 此前不会回落为 Responses 的 `max_output_tokens`，长度上限被静默丢弃（Hermes 等 Chat 客户端必现）；`stream_options.include_usage` 此前不会带过去，请求方要求的最终 usage 分片拿不到，现在按 Responses 同名字段映射。

## v1.9.1 - 2026-08-12

- 修复 OpenAI Chat 入站（`/v1/chat/completions`）声明 `web_search` 打 Anthropic 协议渠道（MiniMax 官方等）时上游根本不执行搜索的问题。Anthropic 只接受带日期后缀的服务端工具类型（`web_search_20250305`），网关此前只在 Anthropic 入站保留客户端发来的原始 spec，其它入站没有 spec 就把整个服务端工具丢掉，只留一条 `transformer.anthropic.server_tool.missing_spec` warn 日志：请求照常成功，但答案没有任何来源，调用方据此判定搜索失败并降级到自己的兜底搜索（如 `fallback_reason: grok_sources_empty`）。现在出站会按类型补一份最小合法 spec 并带上对应 beta 头，覆盖 Chat 的 `web_search`、Responses 的 `web_search_preview`、Gemini 的 `server_search` 与 code execution 的同类写法；工具类型未知或缺名字时仍然丢弃并告警。
- 搜索来源现在能回到 OpenAI 协议的客户端：网关从上游的 `web_search_tool_result` 结果块与 text 块上的 `citations` 中提取来源，同时回填 `message.annotations`（`url_citation` 形态，含 `cited_text` 与字符区间）与 `message.search_sources`（含 `title`、`page_age`），两者缺一时相互兜底，流式下随分片下发并在聚合时按 URL 去重。同时修正三处连带的丢失：响应侧 `citations` 是数组、而请求侧 document 块的同名字段是对象，此前一律按对象解析，带引用的整个响应体会解析失败；`web_search_result` 的 `url` / `page_age` / `encrypted_content` 不在内容块字段集里，解析即丢，多轮对话回放搜索结果时也一并丢失；OpenAI 响应的 `content` 里不再残留 `server_tool_use` / `server_tool_result` 空壳，只留正文文本。

## v1.9 - 2026-08-10

- 渠道的「同步过滤正则」从「高级设置」移到「已选模型」下方，文案改成它的真实作用：过滤自动同步与手动刷新时上游返回的模型清单，只保留匹配项，自定义模型不受影响（此前写的是「用于匹配请求模型名称」，与实现不符，加上位置隐蔽，基本没人找得到）。输入时实时校验，非法正则会禁用刷新按钮并被后端以 400 拒绝——此前正则写错会让每轮自动同步在拉取阶段直接失败，只留一条 warn 日志，渠道从此静默不再同步。
- 自动分组的「继承全局」由创建时快照改为实时跟随：渠道新增「跟随全局」状态并作为新建渠道的默认值，之后修改「全局默认模式」时，所有没单独设置过的渠道自动跟着变，不必再进对话框逐个选匹配方式。单独设过具体模式或「不自动分组」的渠道不受影响；站点投影渠道仍由全局强制覆盖，行为不变；存量渠道保持原值，不做迁移。
- 首页排行榜的 Tokens 列在中文界面改用万/亿（如 3001万、1.5亿），不足一千显示原数字，英文界面仍为 K/M/B。

## v1.8.5 - 2026-08-08

- 修复 OpenAI Chat 入站（`/v1/chat/completions`）打 DeepSeek 等 thinking 模式渠道时多轮工具调用被上游以 “The `reasoning_content` in the thinking mode must be passed back to the API.” 拒绝的问题。Chat 协议本身允许客户端回传 `reasoning_content`，但部分客户端（如 Hermes）会对没有思维过程的轮次填一个空格占位，被上游当作缺失处理。现在网关按 tool call ID 自行存档推理正文并在重放时回填，与 v1.8.3 在 Responses 入站上的处理复用同一套实现。

## v1.8.4 - 2026-08-05

- 修复网关向上游发送对方 schema 之外的请求字段、被严格上游以未知字段拒绝整轮请求的问题（此前靠渠道 failover 兜底）。其一，v1.7 引入的「同格式保留未建模顶层参数」会把客户端的驼峰写法原样转发，如 `promptCacheKey` 被基元律动以 `UNKNOWN_FIELD` 返回 400，而网关建模的字段名是 `prompt_cache_key`，该值既没被识别也没被丢弃；现在驼峰键会按对应的 snake_case 正名收编，真正未建模的新参数仍照旧保留。其二，请求体里 `assistant.tool_calls` 会多带一个 `index` 字段，它不在 OpenAI Chat 的请求 schema 内，现已在出站剥离，流式响应侧不受影响。

## v1.8.3 - 2026-08-04

- 修复 OpenAI Responses 入站（`/v1/responses`）打 DeepSeek 等 thinking 模式渠道时多轮工具调用被上游以 “The `reasoning_content` in the thinking mode must be passed back to the API.” 拒绝的问题。Responses 协议只回显推理摘要与 `encrypted_content`，推理正文出站即丢失，客户端自然也带不回来。现在网关按 tool call ID 自行缓存推理正文并在重放时回填，与 v1.8.1 的 Gemini `thoughtSignature` 缓存同一套路，客户端无需感知。

## v1.8.2 - 2026-08-04

- 修复 OpenAI Responses 入站（`/v1/responses`）转发到 Chat 协议渠道时被上游以 `Invalid assistant message: content or tool_calls must be set` 拒绝的问题。Responses 把 assistant 文本、`reasoning` 和 `function_call` 拆成相邻的独立条目，转换时会产出既无 `content` 又无 `tool_calls` 的空 assistant 消息：宽松的上游忽略，严格的上游直接 400。现在 `function_call` 会并入紧邻的 assistant 消息，只带推理内容的孤立条目在推理并入后一条 assistant 消息后剔除。opencode 等客户端在多轮工具调用后必现此问题。

## v1.8.1 - 2026-08-04

- 修复 OpenAI Chat 入站（`/v1/chat/completions`）打 Gemini 3 渠道时多轮工具调用被上游拒绝的问题：Chat 协议没有承载 `thoughtSignature` 的字段，签名随响应发出即被丢弃，客户端回传历史 `tool_calls` 时缺签名，Gemini 以 `Function call is missing a thought_signature` 返回 400。现在网关按 tool call ID 自行缓存并在重放时回填，与 Anthropic 入站行为一致，客户端无需感知。

## v1.8 - 2026-08-03

- 渠道新增「轮询」开关（API Key 标题右侧）：开启后同一渠道内多个 API Key 在请求间轮流使用，关闭时保持按累计成本最低优先。可缓解单渠道多 key 在成本相同时总命中第一个 key 的问题。
- 新增网关侧 web search 支持：上游响应包含 provider-native 的 `web_search` / `web_search_preview` / Anthropic `web_search_*` server tool 时，网关自行执行搜索并把结果作为 tool result 重放，无需客户端脚手架。新增 `web_search_enabled`（默认开启）与 `web_search_max_rounds`（默认 3）两项设置，分别控制是否执行与最大重放轮数。

## v1.7.2 - 2026-08-02

- 自动分组配置对话框的渠道列表新增全选勾选框，可一次性选中当前搜索结果内的全部渠道再批量设置匹配方式。
- 新建渠道未指定自动分组时默认沿用「全局默认模式」，不再落在「不自动分组」上等待事后逐个补规则。

## v1.7.1 - 2026-08-01

- 修复站点同步重写分组条目时撞唯一索引导致整轮同步中断的问题：迁移目标已存在同名投影记录时合并而非硬改，单条失败也不再连带跳过后续清理与缓存刷新。

## v1.7 - 2026-07-30

- 新增 Codex 渠道类型，复用 OpenAI Responses 协议并自动注入 Codex 特征请求头。
- 引入 Transformer IR 请求 Hook，为渠道/模型兼容修补提供可扩展入口。
- OpenAI Chat 同格式转发保留未建模的顶层参数，减少新参数被静默丢弃的问题。
- 明确 relay 与 transformer 的转换边界，便于后续维护。
