# MaiBot Go Migration

Reference: MaiBot `062d45cf8bd4272ac57d7ec03fcf405e5a458211`, specifically
`src/webui/routes.py` and its registered router modules. The new UI is Vue/
Art Design Pro, control and chat computation are Go. No Python runtime is used.

| Upstream capability | New location | Current delivery | Remaining acceptance |
| --- | --- | --- | --- |
| Bot accounts / identity | Topbar + account-scoped native login | Independent workspace panels and preserved view state | Native receive/reconnect and real login acceptance |
| Persona / reply style | AI 人设 | Versioned per-account configuration | Prompt behavior comparison and generation-time changes |
| Chat settings | 聊天配置 | Go validation, sealed storage, CAS, account-scoped drafts | Apply immutable settings in the native event-to-engine path |
| Model configuration | 模型服务 | Sealed API key; masked reads; external Go HTTP provider | Provider contract/usage accounting/vision/embedding execution |
| Active reply / scheduling | `internal/heatbot` | Four workers; per-group serialization; one merged pending candidate; deadline/cancel | Native inbox routing, freshness and final control validation |
| Statistics / reasoning | 运行统计 | Explicit capability status, no fake runtime metrics | Scoped token ledger, vendor usage, timing and safe traces |
| People / impressions | 人物与印象 | Migration status, no global read path | Go group-scoped storage, deletion, retention and retrieval |
| Expressions / jargon / text learning | 表达与黑话 | Account defaults stored; cross-group settings rejected | Go extraction, group-scoped storage and compare fixtures |
| Long-term memory / graph / search | 长期记忆 | Retention/default validation; scoped request contract | Go group-only persistence, vector index, graph and isolation tests |
| Emoji / avatar / user emoji / media | 表情与媒体 | Migration status, no unscoped media API | Scoped object storage, vision pipeline and expiry |
| Plugins / MCP / agent tools | 插件与工具 | Disabled and visibly blocked | Go plugin contracts, allowlist, tenant isolation and safe tool subset |
| Logs / websocket events | 引擎操作记录 | Safe configuration audit, per-account filtering | Scoped runtime event stream and redaction |
| Data transfer / backup | 备份与迁移 | Docker database backup and migration runbook | Scoped derived-data export/import and retention-aware restore |
| System / self update | 引擎与升级 | Docker release workflow; no in-app shell/update | Release/rollback rehearsal and complete parity approval |
| News / upstream search service | No automatic external feed | Not connected | Decide whether necessary for the product; never expose tenant prompts |

Settings-only panels save real versioned records but explicitly return
`applied=false`. Learning and memory configuration does not create a learning
pipeline or weaken group isolation. Engine health reports `parity_verified=false`.
Normal platform operation cannot enable test generation through WebUI.

Acceptance order: scoped message/model contract and queue; explicit persistent
memory boundaries; learning/people/media; usage and traces; plugin safe subset;
native event routing and control submission; parity and resource soak tests.
The Go replacement must not be described as a completed MaiBot port until all
required rows have executable comparison fixtures and recorded results.
