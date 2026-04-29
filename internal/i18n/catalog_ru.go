package i18n

func init() {
	register(LocaleRU, map[string]string{
		// Common validation
		MsgRequired:         "%s обязательно для заполнения",
		MsgInvalidID:        "недопустимый %s ID",
		MsgNotFound:         "%s не найден(о): %s",
		MsgAlreadyExists:    "%s уже существует: %s",
		MsgInvalidRequest:   "недопустимый запрос: %s",
		MsgInvalidJSON:      "неверный JSON",
		MsgUnauthorized:     "не авторизован",
		MsgPermissionDenied: "доступ запрещен: %s",
		MsgInternalError:    "внутренняя ошибка: %s",
		MsgInvalidSlug:      "%s должен быть правильным слагом (только строчные буквы, цифры и дефисы)",
		MsgFailedToList:     "не удалось получить список %s",
		MsgFailedToCreate:   "не удалось создать %s: %s",
		MsgFailedToUpdate:   "не удалось обновить %s: %s",
		MsgFailedToDelete:   "не удалось удалить %s: %s",
		MsgFailedToSave:     "не удалось сохранить %s: %s",
		MsgInvalidUpdates:   "недопустимые обновления",

		// Agent
		MsgAgentNotFound:       "агент не найден: %s",
		MsgCannotDeleteDefault: "невозможно удалить агента по умолчанию",
		MsgUserCtxRequired:     "требуется контекст пользователя",

		// Chat
		MsgRateLimitExceeded: "превышен лимит запросов — пожалуйста, подождите",
		MsgNoUserMessage:     "сообщение пользователя не найдено",
		MsgUserIDRequired:    "user_id обязателен",
		MsgMsgRequired:       "сообщение обязательно",

		// Abort
		MsgAbortStopped:         "выполнение остановлено",
		MsgAbortForced:          "выполнение принудительно прервано (превышено время ожидания 3с)",
		MsgAbortAlreadyAborting: "остановка уже выполняется",
		MsgAbortNotFound:        "запуск не найден или уже завершен",
		MsgAbortUnauthorized:    "нет прав для прерывания этого выполнения",
		MsgAbortFailed:          "не удалось прервать выполнение: %s",

		// Channel instances
		MsgInvalidChannelType: "недопустимый channel_type",
		MsgInstanceNotFound:   "экземпляр не найден",

		// Cron
		MsgJobNotFound:     "задача не найдена",
		MsgInvalidCronExpr: "недопустимое cron-выражение: %s",

		// Config
		MsgConfigHashMismatch: "конфигурация была изменена (несовпадение хеша)",

		// Exec approval
		MsgExecApprovalDisabled: "подтверждение выполнения не включено",

		// Pairing
		MsgSenderChannelRequired: "требуются senderId и channel",
		MsgCodeRequired:          "требуется код",
		MsgSenderIDRequired:      "sender_id обязателен",

		// HTTP API
		MsgInvalidAuth:           "недопустимая аутентификация",
		MsgMsgsRequired:          "требуется массив messages",
		MsgUserIDHeader:          "требуется заголовок X-GoClaw-User-Id",
		MsgFileTooLarge:          "файл слишком большой или имеет неверный формат multipart form",
		MsgMissingFileField:      "отсутствует поле 'file'",
		MsgInvalidFilename:       "недопустимое имя файла",
		MsgChannelKeyReq:         "требуются channel и key",
		MsgMethodNotAllowed:      "метод не разрешен",
		MsgStreamingNotSupported: "потоковая передача не поддерживается",
		MsgOwnerOnly:             "только владелец может %s",
		MsgNoAccess:              "нет доступа к %s",
		MsgAlreadySummoning:      "агент уже призывается",
		MsgSummoningUnavailable:  "призыв недоступен",
		MsgNoDescription:         "у агента нет описания для повторного призыва",
		MsgSummonCancelled:       "призыв отменен пользователем",
		MsgCannotCancel:          "агент не вызывается",
		MsgInvalidPath:           "недопустимый путь",

		// Tenant backup / restore
		MsgRestoreNewModeRejectsTenantID: "режим mode=new создает нового тенанта; передайте tenant_slug (а не tenant_id) в качестве slug для нового тенанта",

		// Scheduler
		MsgQueueFull:    "очередь сессий заполнена",
		MsgShuttingDown: "шлюз выключается, пожалуйста, повторите попытку позже",

		// Provider
		MsgProviderReqFailed: "%s: ошибка запроса: %s",

		// Unknown method
		MsgUnknownMethod: "неизвестный метод: %s",

		// Not implemented
		MsgNotImplemented: "%s еще не реализован(о)",

		// Agent links
		MsgLinksNotConfigured: "связи агента не настроены",
		MsgInvalidDirection:   "направление должно быть исходящим (outbound), входящим (inbound) или двунаправленным (bidirectional)",
		MsgSourceTargetSame:   "источник и цель должны быть разными агентами",
		MsgCannotDelegateOpen: "невозможно делегировать открытым агентам — только предварительно заданные агенты могут быть целями для делегирования",
		MsgNoUpdatesProvided:  "обновления не предоставлены",
		MsgInvalidLinkStatus:  "статус должен быть 'active' (активен) или 'disabled' (отключен)",

		// Teams
		MsgTeamsNotConfigured:   "команды не настроены",
		MsgAgentIsTeamLead:      "агент уже является лидером команды",
		MsgCannotRemoveTeamLead: "невозможно удалить лидера команды",

		// Channels
		MsgCannotDeleteDefaultInst: "невозможно удалить экземпляр канала по умолчанию",
		MsgCannotRemoveLastWriter:  "невозможно удалить последнего писателя файлов",

		// Skills
		MsgSkillsUpdateNotSupported: "обновление навыков не поддерживается для навыков на основе файлов",
		MsgCannotResolveSkillID:     "не удалось определить ID навыка для файлового навыка",

		// Logs
		MsgInvalidLogAction: "действие должно быть 'start' (запустить) или 'stop' (остановить)",

		// Config
		MsgRawConfigRequired:     "требуется сырая конфигурация (raw config)",
		MsgRawPatchRequired:      "требуется сырой патч (raw patch)",
		MsgConfigMasterScopeOnly: "методы config.* доступны только в мастере; используйте эндпоинты конфигурации инструмента тенанта для переопределений на уровне тенанта",
		MsgMasterScopeRequired:   "это действие требует области видимости мастер-тенанта",

		// Storage / File
		MsgCannotDeleteSkillsDir: "невозможно удалить директории навыков",
		MsgFailedToReadFile:      "не удалось прочитать файл",
		MsgFileNotFound:          "файл не найден",
		MsgInvalidVersion:        "недопустимая версия",
		MsgVersionNotFound:       "версия не найдена",
		MsgFailedToDeleteFile:    "не удалось удалить",

		// OAuth
		MsgNoPendingOAuth:    "нет ожидающего процесса OAuth",
		MsgFailedToSaveToken: "не удалось сохранить токен",

		// Intent Classify
		MsgStatusWorking:       "🔄 Работаю над вашим запросом... Пожалуйста, подождите.",
		MsgStatusDetailed:      "🔄 Сейчас работаю над вашим запросом...\n%s (итерация %d)\nВыполняется: %s\n\nПожалуйста, подождите — я отвечу, как только закончу.",
		MsgStatusPhaseThinking: "Фаза: Думаю...",
		MsgStatusPhaseToolExec: "Фаза: Выполнение %s",
		MsgStatusPhaseTools:    "Фаза: Использование инструментов...",
		MsgStatusPhaseCompact:  "Фаза: Сжатие контекста...",
		MsgStatusPhaseDefault:  "Фаза: Обработка...",
		MsgCancelledReply:      "✋ Отменено. Что бы вы хотели сделать дальше?",
		MsgInjectedAck:         "Понял, я учту это в текущей задаче.",

		// Knowledge Graph
		MsgEntityIDRequired:       "требуется entity_id",
		MsgEntityFieldsRequired:   "требуются external_id, name и entity_type",
		MsgTextRequired:           "требуется текст",
		MsgProviderModelRequired:  "требуются provider и model",
		MsgInvalidProviderOrModel: "недопустимый провайдер или модель",

		// Builtin tool descriptions
		MsgToolReadFile:        "Прочитать содержимое файла в рабочей области агента по пути",
		MsgToolWriteFile:       "Записать содержимое в файл в рабочей области, создав директории при необходимости",
		MsgToolListFiles:       "Показать файлы и директории по заданному пути в рабочей области",
		MsgToolEdit:            "Применить точечные изменения путем поиска и замены в существующих файлах без полной перезаписи",
		MsgToolExec:            "Выполнить команду оболочки (shell) в рабочей области и вернуть stdout/stderr",
		MsgToolWebSearch:       "Поиск информации в интернете через поисковую систему (Brave или DuckDuckGo)",
		MsgToolWebFetch:        "Загрузить веб-страницу или API эндпоинт и извлечь текст",
		MsgToolMemorySearch:    "Поиск в долгосрочной памяти агента с использованием семантического сходства",
		MsgToolMemoryGet:       "Получить конкретный документ из памяти по его пути",
		MsgToolKGSearch:        "Поиск сущностей, связей и наблюдений в графе знаний агента",
		MsgToolReadImage:       "Анализ изображений с использованием визуальных LLM",
		MsgToolReadDocument:    "Анализ документов (PDF, Word, Excel, PowerPoint, CSV и т.д.)",
		MsgToolCreateImage:     "Генерация изображений по тексту",
		MsgToolReadAudio:       "Анализ аудиофайлов (речь, музыка, звуки)",
		MsgToolReadVideo:       "Анализ видеофайлов",
		MsgToolCreateVideo:     "Генерация видео по текстовому описанию",
		MsgToolCreateAudio:     "Генерация музыки или звуковых эффектов по тексту",
		MsgToolTTS:             "Преобразование текста в естественно звучащую речь",
		MsgToolBrowser:         "Автоматизация работы в браузере: навигация, клики, заполнение форм, скриншоты",
		MsgToolSessionsList:    "Список активных сессий во всех каналах",
		MsgToolSessionStatus:   "Текущий статус и метаданные конкретной сессии",
		MsgToolSessionsHistory: "История сообщений конкретной чат-сессии",
		MsgToolSessionsSend:    "Отправить сообщение в активную сессию от имени агента",
		MsgToolMessage:         "Отправить проактивное сообщение пользователю в подключенный канал (Telegram, Discord и т.д.)",
		MsgToolCron:            "Планирование или управление повторяющимися задачами (cron) или таймерами",
		MsgToolSpawn:           "Запуск подагента для фоновой работы или делегирование задачи связанному агенту",
		MsgToolSkillSearch:     "Поиск доступных навыков по ключевому слову или описанию",
		MsgToolUseSkill:        "Активировать навык для использования его возможностей (маркер трассировки)",
		MsgToolSkillManage:     "Создание, изменение или удаление навыков из чата",
		MsgToolPublishSkill:    "Регистрация директории с навыком в базе данных, чтобы он стал доступен для поиска",
		MsgToolTeamTasks:       "Просмотр, создание, обновление и выполнение задач на командной доске",

		MsgSkillNudgePostscript: "Эта задача состояла из нескольких шагов. Хотите, чтобы я сохранил этот процесс как переиспользуемый навык? Ответьте **\"save as skill\"** или **\"skip\"**.",
		MsgSkillNudge70Pct:      "[Система] Вы исчерпали бюджет итераций на 70%. Подумайте, стоит ли сохранить паттерны этой сессии в качестве навыка.",
		MsgSkillNudge90Pct:      "[Система] Вы исчерпали бюджет итераций на 90%. Если эта сессия включает в себя повторяемые действия, сохраните их как навык перед завершением.",

		MsgInvalidRole: "недопустимая роль: разрешены значения owner (владелец), admin (админ), operator (оператор), member (участник), viewer (читатель)",

		MsgContactIDsRequired:  "требуется contact_ids",
		MsgMergeTargetRequired: "должно быть указано только одно из: tenant_user_id или create_user",
		MsgTenantUserNotFound:  "пользователь тенанта не найден",
		MsgTenantMismatch:      "пользователь тенанта не принадлежит данному тенанту",
		MsgTenantScopeRequired: "для этой операции требуется область видимости тенанта",

		// TTS / Voices
		MsgTtsUnknownModel:       "неизвестная TTS модель: %s",
		MsgVoicesListFailed:      "не удалось получить список голосов: %s",
		MsgTtsGeminiInvalidVoice: "неверный голос Gemini: %s",
		MsgTtsGeminiSpeakerLimit: "Gemini TTS поддерживает не более 2 говорящих",
		MsgTtsGeminiInvalidModel:  "неверная модель Gemini TTS: %s",
		MsgTtsGeminiTextOnly:      "Gemini отказался генерировать аудио. Попробуйте более простой текст без перевода или комментариев.",
		MsgTtsParamOutOfRange:     "Значение TTS параметра %q %v вне допустимого диапазона [%v, %v]",
		MsgTtsParamUnknownKey:     "Параметр TTS %q не поддерживается данным провайдером",
		MsgTtsMiniMaxVoicesFailed: "не удалось получить голоса MiniMax: %s",

		// STT
		MsgSTTAllProvidersFailed:     "Все провайдеры STT завершились с ошибкой",
		MsgSTTLegacyConfigDeprecated: "Устаревшая конфигурация STT (deprecated); перейдите на builtin_tools[stt]",
		MsgSTTWhatsappPrivacyWarning: "Включение STT для WhatsApp нарушает сквозное (end-to-end) шифрование для голосовых сообщений, отправленных этому агенту.",
		MsgVoiceMessageFallback:      "[Голосовое сообщение]",

		// Hooks
		MsgHookInvalidMatcher:          "недопустимое регулярное выражение matcher'а: %s",
		MsgHookCommandDisabledStandard: "хуки командного типа доступны только в редакции Lite",
		MsgHookPromptRequiresMatcher:   "промпт-хуки требуют matcher или if_expr (защита от неконтролируемых расходов)",
		MsgHookCircuitBreakerTripped:   "хук был автоматически отключен после серии ошибок",
		MsgHookBudgetExceeded:          "превышен бюджет токенов хука для тенанта",
		MsgHookPerTurnCapReached:       "достигнут лимит вызовов хуков на один ход (turn)",
		MsgHookBuiltinReadOnly:         "встроенные хуки доступны только для чтения (за исключением переключателя 'enabled')",

		// Message tool cross-target forward notice
		MessageCrossTargetForwarded: "📤 Переслано в %s по запросу: %q",
	})
}
