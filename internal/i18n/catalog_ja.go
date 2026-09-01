package i18n

func init() {
	register(LocaleJA, map[string]string{
		// Common validation
		MsgRequired:         "%sは必須です",
		MsgInvalidID:        "%s IDが無効です",
		MsgNotFound:         "%sが見つかりません: %s",
		MsgAlreadyExists:    "%sはすでに存在します: %s",
		MsgInvalidRequest:   "リクエストが無効です: %s",
		MsgInvalidJSON:      "JSONが無効です",
		MsgUnauthorized:     "認証されていません",
		MsgPermissionDenied: "権限が拒否されました: %sに対するロールが不足しています",
		MsgInternalError:    "内部エラー: %s",
		MsgInvalidSlug:      "%sは有効なスラグ（小文字、数字、ハイフンのみ）でなければなりません",
		MsgFailedToList:     "%sの一覧取得に失敗しました",
		MsgFailedToCreate:   "%sの作成に失敗しました: %s",
		MsgFailedToUpdate:   "%sの更新に失敗しました: %s",
		MsgFailedToDelete:   "%sの削除に失敗しました: %s",
		MsgFailedToSave:     "%sの保存に失敗しました: %s",
		MsgInvalidUpdates:   "更新内容が無効です",

		// Agent
		MsgAgentNotFound:       "エージェントが見つかりません: %s",
		MsgCannotDeleteDefault: "デフォルトエージェントは削除できません",
		MsgUserCtxRequired:     "ユーザーコンテキストが必要です",

		// Chat
		MsgRateLimitExceeded: "レート制限を超えました — しばらくお待ちください",
		MsgNoUserMessage:     "ユーザーメッセージが見つかりません",
		MsgUserIDRequired:    "user_idは必須です",
		MsgMsgRequired:       "メッセージは必須です",

		// Channel instances
		MsgInvalidChannelType: "channel_typeが無効です",
		MsgInstanceNotFound:   "インスタンスが見つかりません",

		// Cron
		MsgJobNotFound:     "ジョブが見つかりません",
		MsgInvalidCronExpr: "cron式が無効です: %s",

		// Config
		MsgConfigHashMismatch: "設定が変更されています（ハッシュ不一致）",

		// Exec approval
		MsgExecApprovalDisabled: "実行承認が有効になっていません",

		// Pairing
		MsgSenderChannelRequired: "senderIdとchannelは必須です",
		MsgCodeRequired:          "コードは必須です",
		MsgSenderIDRequired:      "sender_idは必須です",

		// HTTP API
		MsgInvalidAuth:           "認証が無効です",
		MsgMsgsRequired:          "messagesは必須です",
		MsgUserIDHeader:          "X-GoClaw-User-Idヘッダーは必須です",
		MsgFileTooLarge:          "ファイルが大きすぎるか、マルチパートフォームが無効です",
		MsgMissingFileField:      "'file'フィールドがありません",
		MsgInvalidFilename:       "ファイル名が無効です",
		MsgChannelKeyReq:         "channelとkeyは必須です",
		MsgMethodNotAllowed:      "メソッドが許可されていません",
		MsgStreamingNotSupported: "ストリーミングはサポートされていません",
		MsgOwnerOnly:             "オーナーのみが%sできます",
		MsgNoAccess:              "この%sへのアクセス権がありません",
		MsgAlreadySummoning:      "エージェントはすでに召喚中です",
		MsgSummoningUnavailable:  "召喚は利用できません",
		MsgNoDescription:         "エージェントに再召喚するための説明がありません",
		MsgInvalidPath:           "パスが無効です",

		// Scheduler
		MsgQueueFull:    "セッションキューが満杯です",
		MsgShuttingDown: "ゲートウェイはシャットダウン中です。しばらくしてから再試行してください",

		// Provider
		MsgProviderReqFailed: "%s: リクエストが失敗しました: %s",

		// Unknown method
		MsgUnknownMethod: "不明なメソッド: %s",

		// Not implemented
		MsgNotImplemented: "%sはまだ実装されていません",

		// Agent links
		MsgLinksNotConfigured:   "エージェントリンクが設定されていません",
		MsgInvalidDirection:     "方向はoutbound、inbound、またはbidirectionalでなければなりません",
		MsgSourceTargetSame:     "ソースとターゲットは異なるエージェントでなければなりません",
		MsgCannotDelegateOpen:   "オープンエージェントに委任できません — 定義済みエージェントのみが委任先になれます",
		MsgNoUpdatesProvided:    "更新内容がありません",
		MsgInvalidLinkStatus:    "ステータスはactiveまたはdisabledでなければなりません",

		// Teams
		MsgTeamsNotConfigured:   "チームが設定されていません",
		MsgAgentIsTeamLead:      "エージェントはすでにチームリーダーです",
		MsgCannotRemoveTeamLead: "チームリーダーを削除できません",

		// Channels
		MsgCannotDeleteDefaultInst: "デフォルトチャンネルインスタンスは削除できません",

		// Skills
		MsgSkillsUpdateNotSupported: "ファイルベースのスキルではskills.updateはサポートされていません",
		MsgCannotResolveSkillID:     "ファイルベースのスキルのスキルIDを解決できません",

		// Logs
		MsgInvalidLogAction: "アクションは'start'または'stop'でなければなりません",

		// Config
		MsgRawConfigRequired: "rawConfigは必須です",
		MsgRawPatchRequired:  "rawPatchは必須です",

		// Storage / File
		MsgCannotDeleteSkillsDir: "スキルディレクトリは削除できません",
		MsgFailedToReadFile:      "ファイルの読み取りに失敗しました",
		MsgFileNotFound:          "ファイルが見つかりません",
		MsgInvalidVersion:        "バージョンが無効です",
		MsgVersionNotFound:       "バージョンが見つかりません",
		MsgFailedToDeleteFile:    "削除に失敗しました",

		// OAuth
		MsgNoPendingOAuth:    "保留中のOAuthフローがありません",
		MsgFailedToSaveToken: "トークンの保存に失敗しました",

		// Intent Classify
		MsgStatusWorking:       "🔄 リクエストを処理中です... しばらくお待ちください。",
		MsgStatusDetailed:      "🔄 リクエストを処理中です...\n%s (繰り返し %d)\n実行時間: %s\n\nしばらくお待ちください — 完了後にお知らせします。",
		MsgStatusPhaseThinking: "フェーズ: 思考中...",
		MsgStatusPhaseToolExec: "フェーズ: %sを実行中",
		MsgStatusPhaseTools:    "フェーズ: ツールを実行中...",
		MsgStatusPhaseCompact:  "フェーズ: コンテキストを圧縮中...",
		MsgStatusPhaseDefault:  "フェーズ: 処理中...",
		MsgCancelledReply:      "✋ キャンセルしました。次に何をしますか？",
		MsgInjectedAck:         "了解しました。現在の作業に反映します。",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_idは必須です",
		MsgEntityFieldsRequired:   "external_id、name、entity_typeは必須です",
		MsgTextRequired:           "textは必須です",
		MsgProviderModelRequired:  "providerとmodelは必須です",
		MsgInvalidProviderOrModel: "providerまたはmodelが無効です",

		// Builtin tool descriptions
		MsgToolReadFile:        "パスでエージェントのワークスペースからファイルの内容を読み取ります",
		MsgToolWriteFile:       "ワークスペースのファイルに内容を書き込み、必要に応じてディレクトリを作成します",
		MsgToolListFiles:       "ワークスペース内の指定パスにあるファイルとディレクトリを一覧表示します",
		MsgToolEdit:            "ファイル全体を書き直さずに、ターゲットを絞った検索・置換編集を既存ファイルに適用します",
		MsgToolExec:            "ワークスペースでシェルコマンドを実行し、標準出力と標準エラーを返します",
		MsgToolWebSearch:       "検索エンジン（BraveまたはDuckDuckGo）を使用してウェブで情報を検索します",
		MsgToolWebFetch:        "ウェブページまたはAPIエンドポイントを取得し、テキストコンテンツを抽出します",
		MsgToolMemorySearch:    "セマンティック類似度を使用してエージェントの長期記憶を検索します",
		MsgToolMemoryGet:       "ファイルパスで特定のメモリドキュメントを取得します",
		MsgToolKGSearch:        "エージェントのナレッジグラフでエンティティ、関係、観察を検索します",
		MsgToolReadImage:       "ビジョン対応のLLMプロバイダーを使用して画像を分析します",
		MsgToolReadDocument:    "ドキュメント対応のLLMプロバイダーを使用してドキュメント（PDF、Word、Excel、PowerPoint、CSVなど）を分析します",
		MsgToolCreateImage:     "画像生成プロバイダーを使用してテキストプロンプトから画像を生成します",
		MsgToolReadAudio:       "音声対応のLLMプロバイダーを使用して音声ファイル（スピーチ、音楽、サウンド）を分析します",
		MsgToolReadVideo:       "ビデオ対応のLLMプロバイダーを使用してビデオファイルを分析します",
		MsgToolCreateVideo:     "AIを使用してテキストの説明からビデオを生成します",
		MsgToolCreateAudio:     "AIを使用してテキストの説明から音楽または効果音を生成します",
		MsgToolTTS:             "テキストを自然な音声に変換します",
		MsgToolBrowser:         "ブラウザ操作を自動化します: ページの移動、要素のクリック、フォームの入力、スクリーンショットの撮影",
		MsgToolSessionsList:    "すべてのチャンネルにわたるアクティブなチャットセッションを一覧表示します",
		MsgToolSessionStatus:   "特定のチャットセッションの現在のステータスとメタデータを取得します",
		MsgToolSessionsHistory: "特定のチャットセッションのメッセージ履歴を取得します",
		MsgToolSessionsSend:    "エージェントに代わってアクティブなチャットセッションにメッセージを送信します",
		MsgToolMessage:         "接続されたチャンネル（Telegram、Discordなど）でユーザーに積極的なメッセージを送信します",
		MsgToolCron:            "cron式、時刻指定、またはインターバルを使用して定期的なタスクをスケジュールまたは管理します",
		MsgToolSpawn:           "バックグラウンド作業のためにサブエージェントを生成するか、リンクされたエージェントにタスクを委任します",
		MsgToolSkillSearch:     "キーワードまたは説明で利用可能なスキルを検索して関連する機能を見つけます",
		MsgToolUseSkill:        "スキルを有効化して特化した機能を使用します（トレースマーカー）",
		MsgToolSkillManage:     "会話の経験からスキルを作成、パッチ適用、または削除します",
		MsgToolPublishSkill:    "スキルディレクトリをシステムデータベースに登録し、検出可能にします",
		MsgToolTeamTasks: "チームタスクボードのタスクを表示、作成、更新、完了します",

		MsgSkillNudgePostscript: "このタスクにはいくつかのステップが必要でした。このプロセスを再利用可能なスキルとして保存しますか？**「スキルとして保存」**または**「スキップ」**と返信してください。",
		MsgSkillNudge70Pct:      "[System] 繰り返し予算の70%に達しました。このセッションのパターンをスキルとして保存するか検討してください。",
		MsgSkillNudge90Pct: "[System] 繰り返し予算の90%に達しました。このセッションに再利用可能なパターンがあれば、完了前にスキルとして保存することを検討してください。",

		// Tenants
		MsgInvalidRole: "ロールが無効です: 許可された値はowner、admin、operator、member、viewerです",

		// Contact merge
		MsgContactIDsRequired:  "contact_idsは必須です",
		MsgMergeTargetRequired: "tenant_user_idまたはcreate_userのいずれか一方が必須です",
		MsgTenantUserNotFound:  "テナントユーザーが見つかりません",
		MsgTenantMismatch:      "テナントユーザーがこのテナントに属していません",
		MsgTenantScopeRequired: "この操作にはテナントスコープが必要です",
	})
}
