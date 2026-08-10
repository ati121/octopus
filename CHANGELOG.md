# 更新日志

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
