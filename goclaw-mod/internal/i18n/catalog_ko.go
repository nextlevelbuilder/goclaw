package i18n

func init() {
	register(LocaleKO, map[string]string{
		// Common validation
		MsgRequired:         "%s은(는) 필수입니다",
		MsgInvalidID:        "잘못된 %s ID",
		MsgNotFound:         "%s을(를) 찾을 수 없습니다: %s",
		MsgAlreadyExists:    "%s이(가) 이미 존재합니다: %s",
		MsgInvalidRequest:   "잘못된 요청: %s",
		MsgInvalidJSON:      "잘못된 JSON",
		MsgUnauthorized:     "인증되지 않음",
		MsgPermissionDenied: "권한이 거부되었습니다: %s",
		MsgInternalError:    "내부 오류: %s",
		MsgInvalidSlug:      "%s은(는) 유효한 슬러그여야 합니다 (소문자, 숫자, 하이픈만 허용)",
		MsgFailedToList:     "%s 목록을 가져오지 못했습니다",
		MsgFailedToCreate:   "%s 생성에 실패했습니다: %s",
		MsgFailedToUpdate:   "%s 업데이트에 실패했습니다: %s",
		MsgFailedToDelete:   "%s 삭제에 실패했습니다: %s",
		MsgFailedToSave:     "%s 저장에 실패했습니다: %s",
		MsgInvalidUpdates:   "잘못된 업데이트",

		// Agent
		MsgAgentNotFound:       "에이전트를 찾을 수 없습니다: %s",
		MsgCannotDeleteDefault: "기본 에이전트는 삭제할 수 없습니다",
		MsgUserCtxRequired:     "사용자 컨텍스트가 필요합니다",

		// Chat
		MsgRateLimitExceeded: "요청 한도 초과 — 잠시 후 다시 시도해주세요",
		MsgNoUserMessage:     "사용자 메시지를 찾을 수 없습니다",
		MsgUserIDRequired:    "user_id가 필요합니다",
		MsgMsgRequired:       "메시지가 필요합니다",

		// Channel instances
		MsgInvalidChannelType: "잘못된 channel_type",
		MsgInstanceNotFound:   "인스턴스를 찾을 수 없습니다",

		// Cron
		MsgJobNotFound:     "작업을 찾을 수 없습니다",
		MsgInvalidCronExpr: "잘못된 크론 표현식: %s",

		// Config
		MsgConfigHashMismatch: "설정이 변경되었습니다 (해시 불일치)",

		// Exec approval
		MsgExecApprovalDisabled: "실행 승인이 활성화되지 않았습니다",

		// Pairing
		MsgSenderChannelRequired: "senderId와 channel이 필요합니다",
		MsgCodeRequired:          "코드가 필요합니다",
		MsgSenderIDRequired:      "sender_id가 필요합니다",

		// HTTP API
		MsgInvalidAuth:           "잘못된 인증",
		MsgMsgsRequired:          "messages가 필요합니다",
		MsgUserIDHeader:          "X-GoClaw-User-Id 헤더가 필요합니다",
		MsgFileTooLarge:          "파일이 너무 크거나 잘못된 멀티파트 양식입니다",
		MsgMissingFileField:      "'file' 필드가 누락되었습니다",
		MsgInvalidFilename:       "잘못된 파일명",
		MsgChannelKeyReq:         "channel과 key가 필요합니다",
		MsgMethodNotAllowed:      "허용되지 않는 메서드",
		MsgStreamingNotSupported: "스트리밍이 지원되지 않습니다",
		MsgOwnerOnly:             "소유자만 %s할 수 있습니다",
		MsgNoAccess:              "이 %s에 대한 접근 권한이 없습니다",
		MsgAlreadySummoning:      "에이전트가 이미 소환 중입니다",
		MsgSummoningUnavailable:  "소환을 사용할 수 없습니다",
		MsgNoDescription:         "에이전트에 재소환할 설명이 없습니다",
		MsgInvalidPath:           "잘못된 경로",

		// Scheduler
		MsgQueueFull:    "세션 대기열이 가득 찼습니다",
		MsgShuttingDown: "게이트웨이가 종료 중입니다. 잠시 후 다시 시도해주세요",

		// Provider
		MsgProviderReqFailed: "%s: 요청에 실패했습니다: %s",

		// Unknown method
		MsgUnknownMethod: "알 수 없는 메서드: %s",

		// Not implemented
		MsgNotImplemented: "%s은(는) 아직 구현되지 않았습니다",

		// Agent links
		MsgLinksNotConfigured:   "에이전트 링크가 설정되지 않았습니다",
		MsgInvalidDirection:     "방향은 outbound, inbound, 또는 bidirectional이어야 합니다",
		MsgSourceTargetSame:     "소스와 대상은 서로 다른 에이전트여야 합니다",
		MsgCannotDelegateOpen:   "오픈 에이전트에게는 위임할 수 없습니다 — 사전 정의된 에이전트만 위임 대상이 될 수 있습니다",
		MsgNoUpdatesProvided:    "업데이트가 제공되지 않았습니다",
		MsgInvalidLinkStatus:    "상태는 active 또는 disabled여야 합니다",

		// Teams
		MsgTeamsNotConfigured:   "팀이 설정되지 않았습니다",
		MsgAgentIsTeamLead:      "에이전트가 이미 팀 리더입니다",
		MsgCannotRemoveTeamLead: "팀 리더는 제거할 수 없습니다",

		// Channels
		MsgCannotDeleteDefaultInst: "기본 채널 인스턴스는 삭제할 수 없습니다",
		MsgCannotRemoveLastWriter:  "마지막 파일 작성자는 제거할 수 없습니다",

		// Skills
		MsgSkillsUpdateNotSupported: "파일 기반 스킬에는 skills.update가 지원되지 않습니다",
		MsgCannotResolveSkillID:     "파일 기반 스킬의 스킬 ID를 확인할 수 없습니다",

		// Logs
		MsgInvalidLogAction: "action은 'start' 또는 'stop'이어야 합니다",

		// Config
		MsgRawConfigRequired:     "원시 설정이 필요합니다",
		MsgRawPatchRequired:      "원시 패치가 필요합니다",
		MsgConfigMasterScopeOnly: "config.* 메서드는 마스터 범위 전용입니다. 테넌트별 재정의는 테넌트 도구 설정 엔드포인트를 사용하세요",

		// Storage / File
		MsgCannotDeleteSkillsDir: "스킬 디렉토리는 삭제할 수 없습니다",
		MsgFailedToReadFile:      "파일 읽기에 실패했습니다",
		MsgFileNotFound:          "파일을 찾을 수 없습니다",
		MsgInvalidVersion:        "잘못된 버전",
		MsgVersionNotFound:       "버전을 찾을 수 없습니다",
		MsgFailedToDeleteFile:    "삭제에 실패했습니다",

		// OAuth
		MsgNoPendingOAuth:    "진행 중인 OAuth 흐름이 없습니다",
		MsgFailedToSaveToken: "토큰 저장에 실패했습니다",

		// Intent Classify
		MsgStatusWorking:       "🔄 요청을 처리 중입니다... 잠시 기다려주세요.",
		MsgStatusDetailed:      "🔄 현재 요청을 처리 중입니다...\n%s (반복 %d)\n실행 시간: %s\n\n잠시 기다려주세요 — 완료되면 응답하겠습니다.",
		MsgStatusPhaseThinking: "단계: 생각 중...",
		MsgStatusPhaseToolExec: "단계: %s 실행 중",
		MsgStatusPhaseTools:    "단계: 도구 실행 중...",
		MsgStatusPhaseCompact:  "단계: 컨텍스트 압축 중...",
		MsgStatusPhaseDefault:  "단계: 처리 중...",
		MsgCancelledReply:      "✋ 취소되었습니다. 다음에 무엇을 하시겠습니까?",
		MsgInjectedAck:         "알겠습니다. 작업 중인 내용에 반영하겠습니다.",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_id가 필요합니다",
		MsgEntityFieldsRequired:   "external_id, name, entity_type이 필요합니다",
		MsgTextRequired:           "텍스트가 필요합니다",
		MsgProviderModelRequired:  "provider와 model이 필요합니다",
		MsgInvalidProviderOrModel: "잘못된 provider 또는 model",

		// Builtin tool descriptions
		MsgToolReadFile:        "경로를 기준으로 에이전트 워크스페이스에서 파일 내용을 읽습니다",
		MsgToolWriteFile:       "필요한 디렉토리를 생성하면서 워크스페이스의 파일에 내용을 씁니다",
		MsgToolListFiles:       "워크스페이스 내 지정된 경로의 파일과 디렉토리를 나열합니다",
		MsgToolEdit:            "전체 파일을 다시 쓰지 않고 기존 파일에 타겟 검색-교체 편집을 적용합니다",
		MsgToolExec:            "워크스페이스에서 셸 명령을 실행하고 stdout/stderr를 반환합니다",
		MsgToolWebSearch:       "검색 엔진(Brave 또는 DuckDuckGo)을 사용하여 웹에서 정보를 검색합니다",
		MsgToolWebFetch:        "웹 페이지 또는 API 엔드포인트를 가져와 텍스트 내용을 추출합니다",
		MsgToolMemorySearch:    "의미론적 유사성을 사용하여 에이전트의 장기 기억을 검색합니다",
		MsgToolMemoryGet:       "파일 경로로 특정 기억 문서를 검색합니다",
		MsgToolKGSearch:        "에이전트의 지식 그래프에서 엔티티, 관계, 관찰을 검색합니다",
		MsgToolReadImage:       "비전 지원 LLM 제공자를 사용하여 이미지를 분석합니다",
		MsgToolReadDocument:    "문서 지원 LLM 제공자를 사용하여 문서(PDF, Word, Excel, PowerPoint, CSV 등)를 분석합니다",
		MsgToolCreateImage:     "이미지 생성 제공자를 사용하여 텍스트 프롬프트에서 이미지를 생성합니다",
		MsgToolReadAudio:       "오디오 지원 LLM 제공자를 사용하여 오디오 파일(음성, 음악, 소리)을 분석합니다",
		MsgToolReadVideo:       "비디오 지원 LLM 제공자를 사용하여 비디오 파일을 분석합니다",
		MsgToolCreateVideo:     "AI를 사용하여 텍스트 설명에서 비디오를 생성합니다",
		MsgToolCreateAudio:     "AI를 사용하여 텍스트 설명에서 음악이나 음향 효과를 생성합니다",
		MsgToolTTS:             "텍스트를 자연스러운 음성 오디오로 변환합니다",
		MsgToolBrowser:         "브라우저 상호작용 자동화: 페이지 탐색, 요소 클릭, 폼 작성, 스크린샷 촬영",
		MsgToolSessionsList:    "모든 채널의 활성 채팅 세션을 나열합니다",
		MsgToolSessionStatus:   "특정 채팅 세션의 현재 상태와 메타데이터를 가져옵니다",
		MsgToolSessionsHistory: "특정 채팅 세션의 메시지 기록을 검색합니다",
		MsgToolSessionsSend:    "에이전트를 대신하여 활성 채팅 세션에 메시지를 보냅니다",
		MsgToolMessage:         "연결된 채널(Telegram, Discord 등)에서 사용자에게 능동적으로 메시지를 보냅니다",
		MsgToolCron:            "크론 표현식, 특정 시간 또는 간격을 사용하여 반복 작업을 예약하거나 관리합니다",
		MsgToolSpawn:           "백그라운드 작업을 위해 하위 에이전트를 생성하거나 연결된 에이전트에 작업을 위임합니다",
		MsgToolSkillSearch:     "키워드나 설명으로 사용 가능한 스킬을 검색하여 관련 기능을 찾습니다",
		MsgToolUseSkill:        "전문화된 기능을 사용하기 위해 스킬을 활성화합니다 (추적 마커)",
		MsgToolSkillManage:     "대화 경험에서 스킬을 생성, 수정 또는 삭제합니다",
		MsgToolPublishSkill:    "스킬 디렉토리를 시스템 데이터베이스에 등록하여 검색 가능하게 만듭니다",
		MsgToolTeamTasks:       "팀 작업 보드에서 작업을 보고, 생성하고, 업데이트하고, 완료합니다",

		MsgSkillNudgePostscript: "이 작업은 여러 단계를 포함했습니다. 이 과정을 재사용 가능한 스킬로 저장할까요? **\"스킬로 저장\"** 또는 **\"건너뛰기\"**로 답장하세요.",
		MsgSkillNudge70Pct:      "[System] 반복 예산의 70%에 도달했습니다. 이 세션의 패턴 중 좋은 스킬이 될 수 있는 것이 있는지 고려해보세요.",
		MsgSkillNudge90Pct:      "[System] 반복 예산의 90%에 도달했습니다. 이 세션에 재사용 가능한 패턴이 포함되어 있다면 완료하기 전에 스킬로 저장하는 것을 고려해보세요.",

		MsgInvalidRole: "잘못된 역할: 허용되는 값은 owner, admin, operator, member, viewer입니다",

		MsgContactIDsRequired:  "contact_ids가 필요합니다",
		MsgMergeTargetRequired: "tenant_user_id 또는 create_user 중 정확히 하나가 필요합니다",
		MsgTenantUserNotFound:  "테넌트 사용자를 찾을 수 없습니다",
		MsgTenantMismatch:      "테넌트 사용자가 이 테넌트에 속하지 않습니다",
		MsgTenantScopeRequired: "이 작업에는 테넌트 범위가 필요합니다",
	})
}
