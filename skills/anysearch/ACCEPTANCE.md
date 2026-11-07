# AnySearch Integration — Acceptance Checklist

Filled against [AnySearch 内置接入资料与审查清单](./anysearch-integration-guide.md snapshot, 2026-09-14) for GoClaw Claim `goclaw#001`.

**Evidence date:** 2026-09-24 (anonymous live smoke + offline fixtures)  
**Request ID verification:** 2026-10-13 (Python/Node local contract tests; search, get_sub_domains, batch_search and extract success output, plus error paths)  
**Integration channel:** REST via bundled skill CLIs (`skills/anysearch/scripts/anysearch_cli.{py,js}`)  
**Client header:** `X-Anysearch-Client: skill/3.1.1`  
**Auth:** `Authorization: Bearer <key>` only when key is non-empty; empty key → anonymous (no empty header)

## Conclusion

**基础内置接入通过**（bundled system skill + docs + live anonymous calls）。  
扩展能力：目录发现 / 垂类 / 批量 / 正文提取 **已接入并实测**；来源选择（`params.*`）**透传已接入，未逐源全量实测**。

---

## 基础内置接入（必须完成）

| # | 要求 | 状态 | 证据 |
|---|------|------|------|
| 1 | 官方代码/正式维护模块含 AnySearch 适配器与注册入口 | ✅ 已接入 | `skills/anysearch/` system skill（`UpsertSystemSkill` 种子，与 `skills/pdf` 同级） |
| 2 | 用户可通过项目正常设置选择 AnySearch，无需自写适配器 | ✅ 已接入 | `/anysearch` / `use_skill`；`docs/anysearch-skill.md` |
| 3 | 安装/发布产物包含该能力，配置文档可用 | ✅ 已接入 | skill 进主仓库随 release/Docker `bundled-skills` 分发；文档见上 |
| 4 | 按保存配置实际调用 AnySearch，失败不冒充成功 | ✅ 已接入 | CLI 直连 `https://api.anysearch.com`；错误分支输出 `Search failed` |
| 5 | 查询词与结果数量准确映射 | ✅ 已接入 | `query` / `max_results` (1–10) → `POST /v1/search` |
| 6 | API Key 可留空，不阻止启用 | ✅ 已接入 | `.env.example` 空值；无 key 可跑 |
| 7 | 无 Key 时匿名请求，不发送空 Authorization | ✅ 已接入 | `anysearch_cli.py:_build_headers` 仅在 `if api_key` 时加 Bearer |
| 8 | 有 Key 时 Bearer，凭据不进普通日志 | ✅ 已接入 | 同上；无打印 key；`.env` gitignore |
| 9 | 返回标题、URL、摘要/内容；区分空结果与错误 | ✅ 已接入 | Markdown 输出 title/url/content；错误非空结果 |
| 10 | HTTP / 业务 code / 鉴权 / 限流 / 超时 / 异常响应处理 | ✅ 已接入 | `ApiError(status, request_id)`；`test_cli.py` 错误路径 |
| 11 | 成功、参数映射、错误分支自动化测试 | ✅ 测试通过 | `python scripts/test_cli.py` → `PASS python`, `PASS node`（本地 stub） |
| 12 | 从项目正常入口完成一次真实调用并保留脱敏证据 | ✅ 实测通过 | 2026-09-24 匿名 live smoke（见下） |

## 扩展能力覆盖

| # | 要求 | 状态 | 证据 |
|---|------|------|------|
| 1 | 领域/子领域目录及参数约束 | ✅ 已接入+实测 | `get_sub_domains --domain code` → `code.doc` / `code.snippet` 及 required 参数 |
| 2 | 显式指定垂类并验证请求 | ✅ 已接入+实测 | `search --domain finance --sub_domain finance.quote --sdp type=stock,symbol=AAPL,cn_code=` |
| 3 | 支持垂类内选择平台/来源 | ⚠️ 已接入（透传）· 未逐源实测 | 任意 `params.*` 经 `--params/--sdp` 透传；未对 161 参数逐项实测 |
| 4 | 批量保留查询-结果对应，单条失败不丢其他 | ✅ 已接入+实测 | `batch_search` Query1/Query2 分别成对输出 |
| 5 | 正文提取 + 失败/格式/截断边界 | ✅ 已接入+实测（基础） | `extract https://example.com` 成功；文档声明 HTML/文本 50k 截断、PDF 等不支持 |
| 6 | 区域、语言等选项传到 AnySearch | ✅ 已接入 | `--zone` / `--language` → REST `zone`/`language` |
| 7 | 脱敏日志/诊断可见 `request_id` | ✅ 已接入+测试 | 服务端返回 `request_id` 时，成功搜索、目录（含无匹配子领域的空目录）、批量每条成功结果和提取响应展示 `Request ID`；API 错误及 200 但响应体格式异常的单条/批量搜索错误保留 `(request_id: …)`。Python/Node 契约测试覆盖上述成功路径、API 错误和格式异常路径 |

## Live smoke（2026-09-24，匿名，无 `ANYSEARCH_API_KEY`）

| 命令 | 结果 |
|------|------|
| `search "hello world" --max_results 1` | ✅ Wikipedia “Hello, world”（~1214ms） |
| `get_sub_domains --domain code` | ✅ 2 sub-domains + 参数 |
| `batch_search --query "OpenAI GPT-4" --query "Go 1.26" --max_results 1` | ✅ 两条结果分别对应 |
| `extract "https://example.com"` | ✅ Markdown + untrusted 提示 |
| `search "AAPL" --domain finance --sub_domain finance.quote --sdp type=stock,symbol=AAPL,cn_code=` | ✅ 报价摘要 |

## 附录子领域（40）覆盖说明

客户端 **不硬编码** 40 个子领域；垂类调用前由 `get_sub_domains` / `GET /v1/sub-domains` 发现。  
审查口径：**通道级已接入**（任意 `tag`/`params` 可调用）；**未逐子领域实测** 的标“未验证”，不宣称号称全量通过。

| 领域（17） | 通道 |
|------------|------|
| general / resource / social_media / finance / academic / legal / health / business / security / ip / code / energy / environment / agriculture / travel / film / gaming | 已接入（发现 + search/batch）· 实测 sample: general, code, finance.quote |

## 非目标（本次不验收）

- 非 MCP 通道（本集成走 REST CLI；MCP 可另行接入）
- 161 个参数的逐项真实数据源可用性
- 服务端未返回 `request_id` 时由客户端生成 ID（客户端仅展示服务端返回的 ID）

## 维护文档

- User guide: `docs/anysearch-skill.md`
- Maintenance: `skills/anysearch/MAINTENANCE.md`
- Upstream pin: `skills/anysearch/UPSTREAM.txt` (`anysearch-ai/anysearch-skill@v3.1.1`)
