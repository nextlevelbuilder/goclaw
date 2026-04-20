package i18n

func init() {
	register(LocaleZH, map[string]string{
		// Common validation
		MsgRequired:         "%s 是必填项",
		MsgInvalidID:        "无效的 %s ID",
		MsgNotFound:         "未找到 %s：%s",
		MsgAlreadyExists:    "%s 已存在：%s",
		MsgInvalidRequest:   "无效请求：%s",
		MsgInvalidJSON:      "无效的 JSON",
		MsgUnauthorized:     "未授权",
		MsgPermissionDenied: "权限不足：%s",
		MsgInternalError:    "内部错误：%s",
		MsgInvalidSlug:      "%s 必须是有效的 slug（小写字母、数字、连字符）",
		MsgFailedToList:     "获取 %s 列表失败",
		MsgFailedToCreate:   "创建 %s 失败：%s",
		MsgFailedToUpdate:   "更新 %s 失败：%s",
		MsgFailedToDelete:   "删除 %s 失败：%s",
		MsgFailedToSave:     "保存 %s 失败：%s",
		MsgInvalidUpdates:   "更新内容无效",

		// Agent
		MsgAgentNotFound:       "未找到Agent：%s",
		MsgCannotDeleteDefault: "无法删除默认Agent",
		MsgUserCtxRequired:     "需要用户上下文",

		// Chat
		MsgRateLimitExceeded: "请求频率超限 — 请稍候",
		MsgNoUserMessage:     "未找到用户消息",
		MsgUserIDRequired:    "user_id 是必填项",
		MsgMsgRequired:       "消息是必填项",

		// Abort
		MsgAbortStopped:         "已停止运行",
		MsgAbortForced:          "已强制中止运行（超过 3 秒宽限期）",
		MsgAbortAlreadyAborting: "正在中止中",
		MsgAbortNotFound:        "运行未找到或已结束",
		MsgAbortUnauthorized:    "无权中止此运行",
		MsgAbortFailed:          "无法中止运行：%s",

		// Channel instances
		MsgInvalidChannelType: "Channel类型无效",
		MsgInstanceNotFound:   "未找到实例",

		// Cron
		MsgJobNotFound:     "未找到任务",
		MsgInvalidCronExpr: "无效的 cron 表达式：%s",

		// Config
		MsgConfigHashMismatch: "配置已更改（hash 不匹配）",

		// Exec approval
		MsgExecApprovalDisabled: "执行审批未启用",

		// Pairing
		MsgSenderChannelRequired: "senderId 和 channel 是必填项",
		MsgCodeRequired:          "代码是必填项",
		MsgSenderIDRequired:      "sender_id 是必填项",

		// HTTP API
		MsgInvalidAuth:           "身份验证无效",
		MsgMsgsRequired:          "messages 是必填项",
		MsgUserIDHeader:          "需要 X-GoClaw-User-Id 请求头",
		MsgFileTooLarge:          "文件过大或 multipart 表单无效",
		MsgMissingFileField:      "缺少 'file' 字段",
		MsgInvalidFilename:       "文件名无效",
		MsgChannelKeyReq:         "channel 和 key 是必填项",
		MsgMethodNotAllowed:      "不允许的请求方法",
		MsgStreamingNotSupported: "不支持流式传输",
		MsgOwnerOnly:             "只有所有者才能%s",
		MsgNoAccess:              "无权访问此%s",
		MsgAlreadySummoning:      "Agent正在被召唤中",
		MsgSummoningUnavailable:  "召唤功能不可用",
		MsgNoDescription:         "Agent没有可供重新召唤的描述",
		MsgSummonCancelled:       "已取消召唤",
		MsgCannotCancel:          "Agent 未处于召唤状态",
		MsgInvalidPath:           "路径无效",

		// Tenant backup / restore
		MsgRestoreNewModeRejectsTenantID: "mode=new 会创建新租户；请传 tenant_slug（而非 tenant_id）作为新租户的 slug",

		// Scheduler
		MsgQueueFull:    "Session队列已满",
		MsgShuttingDown: "网关正在关闭，请稍后重试",

		// Provider
		MsgProviderReqFailed: "%s：请求失败：%s",

		// Unknown method
		MsgUnknownMethod: "未知方法：%s",

		// Not implemented
		MsgNotImplemented: "%s 尚未实现",

		// Agent links
		MsgLinksNotConfigured: "Agent链接未配置",
		MsgInvalidDirection:   "方向必须是 outbound、inbound 或 bidirectional",
		MsgSourceTargetSame:   "源和目标必须是不同的Agent",
		MsgCannotDelegateOpen: "无法委派给开放型Agent — 只有预定义Agent才能作为委派目标",
		MsgNoUpdatesProvided:  "未提供更新内容",
		MsgInvalidLinkStatus:  "状态必须是 active 或 disabled",

		// Teams
		MsgTeamsNotConfigured:   "团队未配置",
		MsgAgentIsTeamLead:      "该Agent已是团队负责人",
		MsgCannotRemoveTeamLead: "无法移除团队负责人",

		// Channels
		MsgCannotDeleteDefaultInst: "无法删除默认Channel实例",
		MsgCannotRemoveLastWriter:  "无法移除最后一个文件管理员",

		// Skills
		MsgSkillsUpdateNotSupported: "基于文件的Skill不支持 skills.update",
		MsgCannotResolveSkillID:     "无法解析基于文件的Skill ID",

		// Logs
		MsgInvalidLogAction: "action 必须是 'start' 或 'stop'",

		// Config
		MsgRawConfigRequired:     "raw 配置是必填项",
		MsgRawPatchRequired:      "raw 补丁是必填项",
		MsgConfigMasterScopeOnly: "config.* 方法仅适用于主作用域；使用租户工具配置端点进行租户级覆盖",
		MsgMasterScopeRequired:   "此操作需要主租户范围",

		// Storage / File
		MsgCannotDeleteSkillsDir: "无法删除Skill目录",
		MsgFailedToReadFile:      "读取文件失败",
		MsgFileNotFound:          "文件未找到",
		MsgInvalidVersion:        "版本无效",
		MsgVersionNotFound:       "未找到该版本",
		MsgFailedToDeleteFile:    "删除失败",

		// OAuth
		MsgNoPendingOAuth:    "没有待处理的 OAuth 流程",
		MsgFailedToSaveToken: "保存令牌失败",

		// Intent Classify
		MsgStatusWorking:       "🔄 我正在处理您的请求...请稍候。",
		MsgStatusDetailed:      "🔄 我正在处理您的请求...\n%s（第 %d 次迭代）\n已运行：%s\n\n请稍候——完成后我会回复您。",
		MsgStatusPhaseThinking: "阶段：思考中...",
		MsgStatusPhaseToolExec: "阶段：正在运行 %s",
		MsgStatusPhaseTools:    "阶段：执行工具中...",
		MsgStatusPhaseCompact:  "阶段：压缩上下文中...",
		MsgStatusPhaseDefault:  "阶段：处理中...",
		MsgCancelledReply:      "✋ 已取消。您接下来想做什么？",
		MsgInjectedAck:         "收到，我会在当前任务中处理。",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_id 是必填项",
		MsgEntityFieldsRequired:   "external_id、name 和 entity_type 是必填项",
		MsgTextRequired:           "text 是必填项",
		MsgProviderModelRequired:  "provider 和 model 是必填项",
		MsgInvalidProviderOrModel: "provider 或 model 无效",

		// 内置工具描述
		MsgToolReadFile:        "按路径读取代理工作区中的文件内容",
		MsgToolWriteFile:       "将内容写入工作区中的文件，自动创建所需目录",
		MsgToolListFiles:       "列出工作区指定路径中的文件和目录",
		MsgToolEdit:            "通过查找和替换对现有文件进行定向编辑，无需重写整个文件",
		MsgToolExec:            "在工作区中执行 shell 命令并返回标准输出/错误",
		MsgToolWebSearch:       "使用搜索引擎（Brave 或 DuckDuckGo）在网络上搜索信息",
		MsgToolWebFetch:        "获取网页或 API 端点并提取其文本内容",
		MsgToolMemorySearch:    "使用语义相似度搜索代理的长期记忆",
		MsgToolMemoryGet:       "按文件路径检索特定的记忆文档",
		MsgToolKGSearch:        "搜索代理知识图谱中的实体、关系和观察记录",
		MsgToolReadImage:       "使用具有视觉能力的 LLM 提供商分析图像",
		MsgToolReadDocument:    "使用 LLM 分析文档（PDF、Word、Excel、PowerPoint、CSV 等）",
		MsgToolCreateImage:     "使用 AI 图像生成提供商从文本提示生成图像",
		MsgToolReadAudio:       "使用具有音频能力的 LLM 分析音频文件（语音、音乐、声音）",
		MsgToolReadVideo:       "使用具有视频能力的 LLM 分析视频文件",
		MsgToolCreateVideo:     "使用 AI 从文本描述生成视频",
		MsgToolCreateAudio:     "使用 AI 从文本描述生成音乐或音效",
		MsgToolTTS:             "将文本转换为自然语音音频",
		MsgToolBrowser:         "自动化浏览器交互：导航页面、点击元素、填写表单、截图",
		MsgToolSessionsList:    "列出所有渠道中的活跃聊天会话",
		MsgToolSessionStatus:   "获取特定聊天会话的当前状态和元数据",
		MsgToolSessionsHistory: "检索特定聊天会话的消息历史",
		MsgToolSessionsSend:    "代理代表向活跃聊天会话发送消息",
		MsgToolMessage:         "在已连接的渠道（Telegram、Discord 等）上向用户主动发送消息",
		MsgToolCron:            "使用 cron 表达式、定时或间隔来调度或管理定期任务",
		MsgToolSpawn:           "创建子代理执行后台工作或将任务委派给已链接的代理",
		MsgToolSkillSearch:     "按关键字或描述搜索可用技能以查找相关功能",
		MsgToolUseSkill:        "激活技能以使用其专门功能（追踪标记）",
		MsgToolSkillManage:     "从对话经验中创建、修补或删除技能",
		MsgToolPublishSkill:    "将技能目录注册到系统数据库中，使其可被发现和授权",
		MsgToolTeamTasks:       "查看、创建、更新和完成团队任务板上的任务",

		MsgSkillNudgePostscript: "此任务涉及多个步骤。要我将此过程保存为可重用技能吗？回复 **\"保存技能\"** 或 **\"跳过\"**。",
		MsgSkillNudge70Pct:      "[System] 您已使用 70% 的迭代预算。请考虑本次会话中的模式是否值得保存为技能。",
		MsgSkillNudge90Pct:      "[System] 您已使用 90% 的迭代预算。如果本次会话涉及可重用的模式，请考虑在完成前将其保存为技能。",

		MsgInvalidRole: "无效角色：允许的值为 owner、admin、operator、member、viewer",

		MsgContactIDsRequired:  "contact_ids 为必填项",
		MsgMergeTargetRequired: "必须提供 tenant_user_id 或 create_user 其中之一",
		MsgTenantUserNotFound:  "未找到租户用户",
		MsgTenantMismatch:      "租户用户不属于此租户",
		MsgTenantScopeRequired: "此操作需要指定租户范围",

		// TTS / 声音
		MsgTtsUnknownModel:       "未知的 tts 模型：%s",
		MsgVoicesListFailed:      "获取声音列表失败：%s",
		MsgTtsGeminiInvalidVoice: "无效的 Gemini 声音：%s",
		MsgTtsGeminiSpeakerLimit: "Gemini TTS 最多支持 2 位发言人",
		MsgTtsGeminiInvalidModel:  "无效的 Gemini TTS 模型：%s",
		MsgTtsParamOutOfRange:     "TTS 参数 %q 的值 %v 超出范围 [%v, %v]",
		MsgTtsParamUnknownKey:     "TTS 参数 %q 不受此提供商支持",
		MsgTtsMiniMaxVoicesFailed: "获取 MiniMax 声音列表失败：%s",

		// STT
		MsgSTTAllProvidersFailed:     "所有 STT 提供商均失败",
		MsgSTTLegacyConfigDeprecated: "旧版 STT 配置已弃用；请迁移至 builtin_tools[stt]",
		MsgSTTWhatsappPrivacyWarning: "为 WhatsApp 启用 STT 将破坏发送至此 Agent 的语音消息的端对端加密。",
		MsgVoiceMessageFallback:      "[语音消息]",

		// Hooks
		MsgHookInvalidMatcher:          "无效的匹配器正则表达式: %s",
		MsgHookCommandDisabledStandard: "命令类型钩子仅在 Lite 版本可用",
		MsgHookPromptRequiresMatcher:   "prompt 钩子必须指定 matcher 或 if_expr(成本失控保护)",
		MsgHookCircuitBreakerTripped:   "钩子在多次失败后已自动禁用",
		MsgHookBudgetExceeded:          "租户钩子令牌预算已耗尽",
		MsgHookPerTurnCapReached:       "单轮钩子调用次数已达上限",
		MsgHookBuiltinReadOnly:         "内置钩子只读,仅允许切换启用状态",

		// Message tool cross-target forward notice
		MessageCrossTargetForwarded: "📤 已按请求转发至 %s:%q",
	})
}
