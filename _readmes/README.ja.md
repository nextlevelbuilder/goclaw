<p align="center">
  <img src="../_statics/goclaw.png" alt="GoClaw" />
</p>

<h1 align="center">GoClaw</h1>

<p align="center"><strong>Enterprise AI Agent Platform</strong></p>

<p align="center">
Multi-agent AI gateway built in Go. 20+ LLM providers. 7 channels. Multi-tenant PostgreSQL.<br/>
Single binary. Production-tested. Agents that orchestrate for you.
</p>

<p align="center">
  <a href="https://docs.goclaw.sh">ドキュメント</a> •
  <a href="https://docs.goclaw.sh/#quick-start">クイックスタート</a> •
  <a href="https://x.com/nlb_io">Twitter / X</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go_1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go" /></a>
  <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL_18-316192?style=flat-square&logo=postgresql&logoColor=white" alt="PostgreSQL" /></a>
  <a href="https://www.docker.com/"><img src="https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker" /></a>
  <a href="https://developer.mozilla.org/en-US/docs/Web/API/WebSocket"><img src="https://img.shields.io/badge/WebSocket-010101?style=flat-square&logo=socket.io&logoColor=white" alt="WebSocket" /></a>
  <a href="https://opentelemetry.io/"><img src="https://img.shields.io/badge/OpenTelemetry-000000?style=flat-square&logo=opentelemetry&logoColor=white" alt="OpenTelemetry" /></a>
  <a href="https://www.anthropic.com/"><img src="https://img.shields.io/badge/Anthropic-191919?style=flat-square&logo=anthropic&logoColor=white" alt="Anthropic" /></a>
  <a href="https://openai.com/"><img src="https://img.shields.io/badge/OpenAI_Compatible-412991?style=flat-square&logo=openai&logoColor=white" alt="OpenAI" /></a>
  <img src="https://img.shields.io/badge/License-MIT-yellow?style=flat-square" alt="License: MIT" />
</p>

**GoClaw** は、LLM をあなたのツール、チャンネル、データに接続するマルチエージェント AI ゲートウェイです。ランタイム依存ゼロの単一 Go バイナリとしてデプロイでき、20以上の LLM プロバイダにまたがるエージェントチームとエージェント間デリゲーションを、完全なマルチテナント分離のもとでオーケストレーションします。

セキュリティ強化、マルチテナント PostgreSQL、本番グレードのオブザーバビリティを備えた [OpenClaw](https://github.com/openclaw/openclaw) の Go 移植版です。

🌐 **Languages:**
[🇺🇸 English](../README.md) ·
[🇨🇳 简体中文](README.zh-CN.md) ·
[🇯🇵 日本語](README.ja.md) ·
[🇰🇷 한국어](README.ko.md) ·
[🇻🇳 Tiếng Việt](README.vi.md) ·
[🇪🇸 Español](README.es.md) ·
[🇧🇷 Português](README.pt.md) ·
[🇫🇷 Français](README.fr.md) ·
[🇩🇪 Deutsch](README.de.md) ·
[🇷🇺 Русский](README.ru.md)
## 他との違い

- **エージェントチームとオーケストレーション** — 共有タスクボード、エージェント間デリゲーション（同期/非同期）、ハイブリッドエージェントディスカバリを備えたチーム
- **マルチテナント PostgreSQL** — ユーザーごとのワークスペース、ユーザーごとのコンテキストファイル、暗号化された API キー（AES-256-GCM）、分離されたセッション
- **単一バイナリ** — 約 25 MB の静的 Go バイナリ、Node.js ランタイム不要、1秒未満で起動、$5 の VPS で動作
- **本番グレードのセキュリティ** — 5層のパーミッションシステム（ゲートウェイ認証 → グローバルツールポリシー → エージェントごと → チャンネルごと → オーナー限定）に加え、レート制限、プロンプトインジェクション検出、SSRF 保護、シェル拒否パターン、AES-256-GCM 暗号化
- **20以上の LLM プロバイダ** — Anthropic（プロンプトキャッシュ付きネイティブ HTTP+SSE）、OpenAI、OpenRouter、Groq、DeepSeek、Gemini、Mistral、xAI、MiniMax、Cohere、Perplexity、DashScope、Bailian、Zai、Ollama、Ollama Cloud、Claude CLI、Codex、ACP、および OpenAI 互換エンドポイント
- **7つのメッセージングチャンネル** — Telegram、Discord、Slack、Zalo OA、Zalo Personal、Feishu/Lark、WhatsApp
- **Extended Thinking** — プロバイダごとの思考モード（Anthropic バジェットトークン、OpenAI 推論努力度、DashScope 思考バジェット）とストリーミングサポート
- **Heartbeat** — HEARTBEAT.md チェックリストによる定期的なエージェントチェックイン、正常時の抑制、アクティブ時間、リトライロジック、チャンネル配信
- **スケジューリングと Cron** — 自動化されたエージェントタスクのための `at`、`every`、cron 式、レーンベースの並列実行
- **オブザーバビリティ** — スパンとプロンプトキャッシュメトリクスを使った LLM コールトレーシングを内蔵、オプションで OpenTelemetry OTLP エクスポート

## Claw エコシステム

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| 言語            | TypeScript      | Rust     | Go       | **Go**                                  |
| バイナリサイズ  | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Docker イメージ | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM（アイドル） | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| 起動時間        | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| 対象ハードウェア| $599+ Mac Mini  | $10 エッジ| $10 エッジ| **$5 VPS+**                            |

| 機能                              | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| --------------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| マルチテナント（PostgreSQL）       | —                                    | —                                            | —                                     | ✅                             |
| MCP 統合                          | — (uses ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| エージェントチーム                 | —                                    | —                                            | —                                     | ✅ タスクボード + メールボックス |
| セキュリティ強化                   | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Basic (workspace restrict, exec deny) | ✅ 5層防御                     |
| OTel オブザーバビリティ            | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| プロンプトキャッシュ               | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| ナレッジグラフ                    | —                                    | —                                            | —                                     | ✅ LLM 抽出 + トラバーサル     |
| スキルシステム                    | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ Basic                              | ✅ BM25 + pgvector ハイブリッド |
| レーンベーススケジューラ           | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/team/cron)   |
| メッセージングチャンネル           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| コンパニオンアプリ                 | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web ダッシュボード              |
| ライブキャンバス / 音声            | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 providers)              |
| LLM プロバイダ                    | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| ユーザーごとのワークスペース       | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| 暗号化されたシークレット           | — (env vars only)                    | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

## アーキテクチャ

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## クイックスタート

**前提条件:** Go 1.26+、pgvector 付き PostgreSQL 18、Docker（オプション）

### ソースから

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactive setup wizard
source .env.local && ./goclaw
```

### Docker を使用する場合

```bash
# Generate .env with auto-generated secrets
chmod +x prepare-env.sh && ./prepare-env.sh

# Add at least one GOCLAW_*_API_KEY to .env, then:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web Dashboard at http://localhost:3000
# Health check: curl http://localhost:18790/health
```

`GOCLAW_*_API_KEY` 環境変数が設定されている場合、ゲートウェイはインタラクティブなプロンプトなしで自動オンボーディングを行います — プロバイダを検出し、マイグレーションを実行し、デフォルトデータをシードします。

> ビルドバリアント（OTel、Tailscale、Redis）、Docker イメージタグ、compose オーバーレイについては、[デプロイメントガイド](https://docs.goclaw.sh/#deploy-docker-compose)を参照してください。

## マルチエージェントオーケストレーション

GoClaw はエージェントチームとエージェント間デリゲーションをサポートしており、各エージェントは独自のアイデンティティ、ツール、LLM プロバイダ、コンテキストファイルで動作します。

### エージェントデリゲーション

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| モード | 動作方法 | 最適な用途 |
|--------|----------|------------|
| **同期（Sync）** | エージェント A がエージェント B に問い合わせ、回答を**待機する** | クイックルックアップ、ファクトチェック |
| **非同期（Async）** | エージェント A がエージェント B に問い合わせ、**処理を続ける**。B は後でアナウンス | 長時間タスク、レポート、詳細分析 |

エージェントは、方向制御（`outbound`、`inbound`、`bidirectional`）とリンクごと・エージェントごとの並列実行制限を持つ明示的な**パーミッションリンク**を通じて通信します。

### エージェントチーム

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **共有タスクボード** — `blocked_by` 依存関係を持つタスクの作成、クレーム、完了、検索
- **チームメールボックス** — ピアツーピアの直接メッセージとブロードキャスト
- **ツール**: タスク管理用の `team_tasks`、メールボックス用の `team_message`

> デリゲーションの詳細、パーミッションリンク、並列実行制御については、[エージェントチームのドキュメント](https://docs.goclaw.sh/#teams-what-are-teams)を参照してください。

## 組み込みツール

| ツール               | グループ      | 説明                                                           |
| -------------------- | ------------- | -------------------------------------------------------------- |
| `read_file`          | fs            | ファイルの内容を読む（仮想 FS ルーティング付き）               |
| `write_file`         | fs            | ファイルの書き込み/作成                                        |
| `edit_file`          | fs            | 既存ファイルへのターゲット編集を適用                           |
| `list_files`         | fs            | ディレクトリの内容を一覧表示                                   |
| `search`             | fs            | パターンでファイル内容を検索                                   |
| `glob`               | fs            | glob パターンでファイルを検索                                  |
| `exec`               | runtime       | シェルコマンドの実行（承認ワークフロー付き）                   |
| `web_search`         | web           | ウェブ検索（Brave、DuckDuckGo）                                |
| `web_fetch`          | web           | ウェブコンテンツの取得と解析                                   |
| `memory_search`      | memory        | 長期メモリの検索（FTS + ベクトル）                             |
| `memory_get`         | memory        | メモリエントリの取得                                           |
| `skill_search`       | —             | スキルの検索（BM25 + 埋め込みハイブリッド）                    |
| `knowledge_graph_search` | memory   | エンティティを検索し、ナレッジグラフの関係をトラバース         |
| `create_image`       | media         | 画像生成（DashScope、MiniMax）                                 |
| `create_audio`       | media         | 音声生成（OpenAI、ElevenLabs、MiniMax、Suno）                  |
| `create_video`       | media         | 動画生成（MiniMax、Veo）                                       |
| `read_document`      | media         | ドキュメントの読み取り（Gemini File API、プロバイダチェーン）  |
| `read_image`         | media         | 画像分析                                                       |
| `read_audio`         | media         | 音声の文字起こしと分析                                         |
| `read_video`         | media         | 動画分析                                                       |
| `message`            | messaging     | チャンネルへのメッセージ送信                                   |
| `tts`                | —             | テキスト読み上げ（Text-to-Speech）合成                         |
| `spawn`              | —             | サブエージェントの生成                                         |
| `subagents`          | sessions      | 実行中のサブエージェントを制御                                 |
| `team_tasks`         | teams         | 共有タスクボード（一覧表示、作成、クレーム、完了、検索）       |
| `team_message`       | teams         | チームメールボックス（送信、ブロードキャスト、読み取り）       |
| `sessions_list`      | sessions      | アクティブなセッションの一覧表示                               |
| `sessions_history`   | sessions      | セッション履歴の表示                                           |
| `sessions_send`      | sessions      | セッションへのメッセージ送信                                   |
| `sessions_spawn`     | sessions      | 新しいセッションの生成                                         |
| `session_status`     | sessions      | セッションステータスの確認                                     |
| `cron`               | automation    | cron ジョブのスケジューリングと管理                            |
| `gateway`            | automation    | ゲートウェイ管理                                               |
| `browser`            | ui            | ブラウザ自動化（ナビゲート、クリック、入力、スクリーンショット）|
| `announce_queue`     | automation    | 非同期結果のアナウンス（非同期デリゲーション用）               |

## ドキュメント

完全なドキュメントは **[docs.goclaw.sh](https://docs.goclaw.sh)** で確認できます。またはソースを [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) で参照してください。

| セクション | トピック |
|------------|----------|
| [はじめに](https://docs.goclaw.sh/#what-is-goclaw) | インストール、クイックスタート、設定、Web ダッシュボードツアー |
| [コアコンセプト](https://docs.goclaw.sh/#how-goclaw-works) | エージェントループ、セッション、ツール、メモリ、マルチテナンシー |
| [エージェント](https://docs.goclaw.sh/#creating-agents) | エージェントの作成、コンテキストファイル、パーソナリティ、共有とアクセス |
| [プロバイダ](https://docs.goclaw.sh/#providers-overview) | Anthropic、OpenAI、OpenRouter、Gemini、DeepSeek、その他 15 以上 |
| [チャンネル](https://docs.goclaw.sh/#channels-overview) | Telegram、Discord、Slack、Feishu、Zalo、WhatsApp、WebSocket |
| [エージェントチーム](https://docs.goclaw.sh/#teams-what-are-teams) | チーム、タスクボード、メッセージング、デリゲーションとハンドオフ |
| [上級](https://docs.goclaw.sh/#custom-tools) | カスタムツール、MCP、スキル、Cron、サンドボックス、フック、RBAC |
| [デプロイメント](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose、データベース、セキュリティ、オブザーバビリティ、Tailscale |
| [リファレンス](https://docs.goclaw.sh/#cli-commands) | CLI コマンド、REST API、WebSocket プロトコル、環境変数 |

## テスト

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (requires running gateway)
```

## プロジェクトステータス

本番環境でテスト済みの内容と進行中の内容を含む詳細な機能ステータスは [CHANGELOG.md](CHANGELOG.md) を参照してください。

## 謝辞

GoClaw はオリジナルの [OpenClaw](https://github.com/openclaw/openclaw) プロジェクトをベースに構築されています。この Go 移植版を着想させたアーキテクチャとビジョンに感謝します。

## ライセンス

MIT
