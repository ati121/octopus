# 更新日志

## v1.7 - 2026-07-30

- 新增 Codex 渠道类型，复用 OpenAI Responses 协议并自动注入 Codex 特征请求头。
- 引入 Transformer IR 请求 Hook，为渠道/模型兼容修补提供可扩展入口。
- OpenAI Chat 同格式转发保留未建模的顶层参数，减少新参数被静默丢弃的问题。
- 明确 relay 与 transformer 的转换边界，便于后续维护。
