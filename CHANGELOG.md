# 更新日志

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
