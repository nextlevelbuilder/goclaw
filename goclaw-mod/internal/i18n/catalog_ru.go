package i18n

func init() {
	register(LocaleRU, map[string]string{
		// Common validation
		MsgRequired:         "требуется %s",
		MsgInvalidID:        "неверный ID %s",
		MsgNotFound:         "%s не найдено: %s",
		MsgAlreadyExists:    "%s уже существует: %s",
		MsgInvalidRequest:   "неверный запрос: %s",
		MsgInvalidJSON:      "неверный JSON",
		MsgUnauthorized:     "не авторизовано",
		MsgPermissionDenied: "доступ запрещён: %s",
		MsgInternalError:    "внутренняя ошибка: %s",
		MsgInvalidSlug:      "%s должно быть корректным слагом (только строчные буквы, цифры и дефисы)",
		MsgFailedToList:     "не удалось получить список %s",
		MsgFailedToCreate:   "не удалось создать %s: %s",
		MsgFailedToUpdate:   "не удалось обновить %s: %s",
		MsgFailedToDelete:   "не удалось удалить %s: %s",
		MsgFailedToSave:     "не удалось сохранить %s: %s",
		MsgInvalidUpdates:   "неверные обновления",

		// Agent
		MsgAgentNotFound:                       "агент не найден: %s",
		MsgCannotDeleteDefault:                 "нельзя удалить агента по умолчанию",
		MsgUserCtxRequired:                     "требуется контекст пользователя",
		MsgGatewayOperatorSecureCLIUnavailable: "Доступ оператора шлюза пропущен, так как хранилище SecureCLI недоступно.",
		MsgGatewayOperatorEligibilityFailed:    "Агент создан, но доступ оператора шлюза не смог подтвердить право на статус первого агента.",
		MsgGatewayOperatorNotFirstAgent:        "Доступ оператора шлюза не предоставлен, так как это не первый агент.",
		MsgGatewayOperatorTokenMissing:         "Доступ оператора шлюза пропущен, так как токен шлюза не настроен.",
		MsgGatewayOperatorBinaryMissing:        "Доступ оператора шлюза пропущен, так как не удалось обнаружить исполняемый файл goclaw.",
		MsgGatewayOperatorExistingReview:       "Доступ оператора шлюза пропущен, так как существующие учётные данные goclaw CLI требуют ручной проверки.",
		MsgGatewayOperatorRegisterFailed:       "Доступ оператора шлюза пропущен, так как не удалось зарегистрировать учётные данные goclaw CLI.",
		MsgGatewayOperatorCredentialFailed:     "Доступ оператора шлюза пропущен, так как не удалось сохранить учётные данные.",

		// Chat
		MsgRateLimitExceeded: "превышен лимит запросов — пожалуйста, подождите",
		MsgNoUserMessage:     "сообщение пользователя не найдено",
		MsgUserIDRequired:    "требуется user_id",
		MsgMsgRequired:       "требуется сообщение",

		// Abort
		MsgAbortStopped:         "выполнение остановлено",
		MsgAbortForced:          "выполнение принудительно прервано (превышен льготный период 3 с)",
		MsgAbortAlreadyAborting: "прерывание уже выполняется",
		MsgAbortNotFound:        "выполнение не найдено или уже завершено",
		MsgAbortUnauthorized:    "нет прав на прерывание этого выполнения",
		MsgAbortFailed:          "не удалось прервать выполнение: %s",

		// Channel instances
		MsgInvalidChannelType: "неверный channel_type",
		MsgInstanceNotFound:   "экземпляр не найден",

		// Cron
		MsgJobNotFound:         "задание не найдено",
		MsgInvalidCronExpr:     "неверное cron-выражение: %s",
		MsgCommandCronDisabled: "команды cron отключены на этом шлюзе (установите cron.command_enabled=true, чтобы разрешить их)",

		// Config
		MsgConfigHashMismatch: "конфигурация изменилась (несоответствие хэша)",

		// Exec approval
		MsgExecApprovalDisabled: "подтверждение выполнения не включено",

		// Pairing
		MsgSenderChannelRequired: "требуются senderId и channel",
		MsgCodeRequired:          "требуется код",
		MsgSenderIDRequired:      "требуется sender_id",

		// HTTP API
		MsgInvalidAuth:            "неверная аутентификация",
		MsgMsgsRequired:           "требуется messages",
		MsgUserIDHeader:           "требуется заголовок X-GoClaw-User-Id",
		MsgFileTooLarge:           "файл слишком большой или неверная multipart-форма",
		MsgMissingFileField:       "отсутствует поле 'file'",
		MsgInvalidFilename:        "неверное имя файла",
		MsgChannelKeyReq:          "требуются channel и key",
		MsgMethodNotAllowed:       "метод не разрешён",
		MsgStreamingNotSupported:  "потоковая передача не поддерживается",
		MsgOwnerOnly:              "только владелец может %s",
		MsgNoAccess:               "нет доступа к этому %s",
		MsgAlreadySummoning:       "агент уже вызывается",
		MsgSummoningUnavailable:   "вызов недоступен",
		MsgRunTimelineUnavailable: "хронология выполнения недоступна",
		MsgNoDescription:          "у агента нет описания для повторного вызова",
		MsgSummonCancelled:        "вызов отменён пользователем",
		MsgCannotCancel:           "агент не вызывается",
		MsgInvalidPath:            "неверный путь",

		// Browser cookies
		MsgBrowserCookieTooMany:            "слишком много cookie браузера в одном запросе синхронизации",
		MsgInvalidCookieURL:                "неверный URL cookie",
		MsgBrowserCookieValueTooLarge:      "значение cookie слишком велико",
		MsgBrowserCookieEncryptionRequired: "шифрование cookie браузера не настроено",

		// Tenant backup / restore
		MsgRestoreNewModeRejectsTenantID: "mode=new создаёт нового арендатора; передайте tenant_slug (а не tenant_id) в качестве целевого слага нового арендатора",

		// Scheduler
		MsgQueueFull:    "очередь сессий заполнена",
		MsgShuttingDown: "шлюз завершает работу, пожалуйста, повторите попытку чуть позже",

		// Provider
		MsgProviderReqFailed: "%s: запрос не выполнен: %s",

		// Usage caps / pricing
		MsgUsageCapsListPoliciesFailed:          "не удалось получить список политик лимитов использования",
		MsgUsageCapPolicyValidationFailed:       "не пройдена проверка политики лимитов использования",
		MsgUsageCapPolicyManaged:                "управляемые политики лимитов использования нельзя изменять",
		MsgUsageCapsDeletePolicyFailed:          "не удалось удалить политику лимитов использования",
		MsgUsageCapsUtilizationFailed:           "не удалось загрузить использование лимитов",
		MsgUsageCapsEventsFailed:                "не удалось загрузить события лимитов использования",
		MsgUsagePricingSyncOpenRouterFailed:     "не удалось синхронизировать цены OpenRouter: %s",
		MsgUsagePricingStoreCatalogFailed:       "не удалось сохранить каталог цен",
		MsgUsagePricingListFailed:               "не удалось получить список цен на модели",
		MsgUsagePricingProviderModelRequired:    "требуются provider_id и model_id",
		MsgUsagePricingOverrideValidationFailed: "не пройдена проверка переопределения цен",
		MsgUsagePricingListOverridesFailed:      "не удалось получить список переопределений цен",
		MsgUsagePricingDeleteOverrideFailed:     "не удалось удалить переопределение цен",

		// Unknown method
		MsgUnknownMethod: "неизвестный метод: %s",

		// Not implemented
		MsgNotImplemented: "%s ещё не реализовано",

		// Agent links
		MsgLinksNotConfigured: "связи агентов не настроены",
		MsgInvalidDirection:   "направление должно быть outbound, inbound или bidirectional",
		MsgSourceTargetSame:   "источник и цель должны быть разными агентами",
		MsgCannotDelegateOpen: "нельзя делегировать открытым агентам — целью делегирования могут быть только предопределённые агенты",
		MsgNoUpdatesProvided:  "обновления не предоставлены",
		MsgInvalidLinkStatus:  "статус должен быть active или disabled",

		// Teams
		MsgTeamsNotConfigured:   "команды не настроены",
		MsgAgentIsTeamLead:      "агент уже является лидом команды",
		MsgCannotRemoveTeamLead: "нельзя удалить лида команды",

		// Channels
		MsgCannotDeleteDefaultInst: "нельзя удалить экземпляр канала по умолчанию",
		MsgCannotRemoveLastWriter:  "нельзя удалить последнего редактора файлов",

		// Skills
		MsgSkillsUpdateNotSupported:    "skills.update не поддерживается для файловых навыков",
		MsgCannotResolveSkillID:        "не удалось определить ID навыка для файлового навыка",
		MsgInvalidVisibility:           "неверная видимость %q: должна быть одной из private, public",
		MsgSkillEvolutionNotConfigured: "хранилище эволюции навыков не настроено",
		MsgActivityStoreNotConfigured:  "хранилище активности не настроено",
		MsgInvalidEvolutionMode:        "неверный режим эволюции",
		MsgSystemSkillMutationBlocked:  "изменение системного навыка заблокировано",
		MsgSuggestionMustBeApproved:    "предложение должно быть одобрено перед применением",
		MsgInvalidDraftPatch:           "неверный draft_patch: %s",
		MsgDraftPatchRequired:          "draft_patch требует content или find/replace",
		MsgFindTextNotFound:            "искомый текст не найден в целевом файле",

		// Logs
		MsgInvalidLogAction: "действие должно быть 'start' или 'stop'",

		// Config
		MsgRawConfigRequired:     "требуется raw-конфигурация",
		MsgRawPatchRequired:      "требуется raw-патч",
		MsgConfigMasterScopeOnly: "методы config.* доступны только в мастер-области; используйте эндпоинты конфигурации инструментов арендатора для переопределений на уровне арендатора",
		MsgMasterScopeRequired:   "это действие требует мастер-области арендатора",

		// Storage / File
		MsgCannotDeleteSkillsDir: "нельзя удалить каталоги навыков",
		MsgFailedToReadFile:      "не удалось прочитать файл",
		MsgFileNotFound:          "файл не найден",
		MsgInvalidVersion:        "неверная версия",
		MsgVersionNotFound:       "версия не найдена",
		MsgFailedToDeleteFile:    "не удалось удалить",

		// OAuth
		MsgNoPendingOAuth:       "нет ожидающего процесса OAuth",
		MsgFailedToSaveToken:    "не удалось сохранить токен",
		MsgOAuthCallbackSuccess: "Авторизация успешна. Вы можете закрыть это окно.",
		MsgOAuthCallbackFailed:  "Авторизация не удалась. Вы можете закрыть это окно.",

		// Intent Classify
		MsgStatusWorking:       "🔄 Я работаю над вашим запросом... Пожалуйста, подождите.",
		MsgStatusDetailed:      "🔄 Я сейчас работаю над вашим запросом...\n%s (итерация %d)\nВремя выполнения: %s\n\nПожалуйста, подождите — я отвечу, когда закончу.",
		MsgStatusPhaseThinking: "Фаза: Размышление...",
		MsgStatusPhaseToolExec: "Фаза: Выполнение %s",
		MsgStatusPhaseTools:    "Фаза: Выполнение инструментов...",
		MsgStatusPhaseCompact:  "Фаза: Сжатие контекста...",
		MsgStatusPhaseDefault:  "Фаза: Обработка...",
		MsgCancelledReply:      "✋ Отменено. Что бы вы хотели сделать дальше?",
		MsgInjectedAck:         "Понял, я учту это в том, над чем работаю.",

		// Knowledge Graph
		MsgEntityIDRequired:       "требуется entity_id",
		MsgEntityFieldsRequired:   "требуются external_id, name и entity_type",
		MsgTextRequired:           "требуется текст",
		MsgProviderModelRequired:  "требуются провайдер и модель",
		MsgInvalidProviderOrModel: "неверный провайдер или модель",

		// Builtin tool descriptions
		MsgToolReadFile:           "Прочитать содержимое файла из рабочей области агента по пути",
		MsgToolWriteFile:          "Записать содержимое в файл в рабочей области, создавая каталоги по мере необходимости",
		MsgToolListFiles:          "Вывести список файлов и каталогов по заданному пути в рабочей области",
		MsgToolEdit:               "Применить точечные правки поиска-и-замены к существующим файлам без переписывания всего файла",
		MsgToolExec:               "Выполнить команду оболочки в рабочей области и вернуть stdout/stderr",
		MsgToolWebSearch:          "Искать информацию в интернете с помощью поисковой системы (Brave или DuckDuckGo)",
		MsgToolWebFetch:           "Загрузить веб-страницу или эндпоинт API и извлечь его текстовое содержимое",
		MsgToolMemorySearch:       "Искать в долговременной памяти агента по семантическому сходству",
		MsgToolMemoryGet:          "Получить конкретный документ памяти по его пути к файлу",
		MsgToolKGSearch:           "Искать сущности, связи и наблюдения в графе знаний агента",
		MsgToolReadImage:          "Анализировать изображения с помощью LLM-провайдера с поддержкой зрения",
		MsgToolReadDocument:       "Анализировать документы (PDF, Word, Excel, PowerPoint, CSV и т. д.) с помощью LLM-провайдера с поддержкой документов",
		MsgToolCreateImage:        "Генерировать изображения из текстовых запросов с помощью провайдера генерации изображений",
		MsgToolReadAudio:          "Анализировать аудиофайлы (речь, музыку, звуки) с помощью LLM-провайдера с поддержкой аудио",
		MsgToolReadVideo:          "Анализировать видеофайлы с помощью LLM-провайдера с поддержкой видео",
		MsgToolCreateVideo:        "Генерировать видео из текстовых описаний с помощью ИИ",
		MsgToolCreateAudio:        "Генерировать музыку или звуковые эффекты из текстовых описаний с помощью ИИ",
		MsgToolTTS:                "Преобразовать текст в естественно звучащую речь",
		MsgToolBrowser:            "Автоматизировать взаимодействие с браузером: переходить по страницам, кликать по элементам, заполнять формы, делать скриншоты",
		MsgToolSessionsList:       "Вывести список активных сессий чата по всем каналам",
		MsgToolSessionStatus:      "Получить текущий статус и метаданные конкретной сессии чата",
		MsgToolSessionsHistory:    "Получить историю сообщений конкретной сессии чата",
		MsgToolSessionsSend:       "Отправить сообщение в активную сессию чата от имени агента",
		MsgToolMessage:            "Отправить проактивное сообщение пользователю по подключённому каналу (Telegram, Discord и т. д.)",
		MsgToolCron:               "Планировать повторяющиеся задачи или управлять ими с помощью cron-выражений, конкретного времени или интервалов",
		MsgToolSpawn:              "Создать субагента для фоновой работы или делегировать задачу связанному агенту",
		MsgToolSkillSearch:        "Искать доступные навыки по ключевому слову или описанию для поиска подходящих возможностей",
		MsgToolUseSkill:           "Активировать навык для использования его специализированных возможностей (маркер трассировки)",
		MsgToolSkillManage:        "Создавать, изменять или удалять навыки на основе опыта разговора",
		MsgToolPublishSkill:       "Зарегистрировать каталог навыка в базе данных системы, сделав его доступным для обнаружения",
		MsgToolTeamTasks:          "Просматривать, создавать, обновлять и завершать задачи на доске задач команды",
		MsgToolAnnouncementSingle: "Я использую %s для следующего шага.",
		MsgToolAnnouncementMulti:  "Я использую %s для следующего шага.",

		MsgSkillNudgePostscript: "Эта задача включала несколько шагов. Хотите, чтобы я сохранил процесс как повторно используемый навык? Ответьте **\"сохранить как навык\"** или **\"пропустить\"**.",
		MsgSkillNudge70Pct:      "[Система] Вы использовали 70% бюджета итераций. Подумайте, могут ли какие-либо шаблоны из этой сессии стать хорошим навыком.",
		MsgSkillNudge90Pct:      "[Система] Вы использовали 90% бюджета итераций. Если эта сессия включала повторно используемые шаблоны, подумайте о сохранении их как навыка до завершения.",

		MsgInvalidRole: "неверная роль: допустимые значения — owner, admin, operator, member, viewer",

		MsgContactIDsRequired:  "требуется contact_ids",
		MsgMergeTargetRequired: "требуется ровно одно из tenant_user_id или create_user",
		MsgTenantUserNotFound:  "пользователь арендатора не найден",
		MsgTenantMismatch:      "пользователь арендатора не принадлежит этому арендатору",
		MsgTenantScopeRequired: "для этой операции требуется область арендатора",

		// TTS / Voices
		MsgTtsUnknownModel:        "неизвестная модель tts: %s",
		MsgVoicesListFailed:       "не удалось получить список голосов: %s",
		MsgTtsGeminiInvalidVoice:  "неверный голос Gemini: %s",
		MsgTtsGeminiSpeakerLimit:  "Gemini TTS поддерживает не более 2 говорящих",
		MsgTtsGeminiInvalidModel:  "неверная модель Gemini TTS: %s",
		MsgTtsGeminiTextOnly:      "Gemini отказался генерировать аудио. Попробуйте более простой текст без перевода или комментариев.",
		MsgTtsParamOutOfRange:     "параметр TTS %q со значением %v выходит за пределы диапазона [%v, %v]",
		MsgTtsParamUnknownKey:     "параметр TTS %q не поддерживается этим провайдером",
		MsgTtsMiniMaxVoicesFailed: "не удалось загрузить голоса MiniMax: %s",

		// STT
		MsgSTTAllProvidersFailed:     "Все провайдеры STT не сработали",
		MsgSTTLegacyConfigDeprecated: "Устаревшая конфигурация STT; перейдите на builtin_tools[stt]",
		MsgSTTWhatsappPrivacyWarning: "Включение STT для WhatsApp нарушает сквозное шифрование голосовых сообщений, отправляемых этому агенту.",
		MsgVoiceMessageFallback:      "[Голосовое сообщение]",

		// Workstation
		MsgWorkstationNotFound:     "рабочая станция не найдена: %s",
		MsgWorkstationKeyExists:    "ключ рабочей станции уже используется: %s",
		MsgInvalidBackend:          "неверный тип бэкенда: %s (должен быть ssh|docker)",
		MsgWorkstationInactive:     "рабочая станция неактивна: %s",
		MsgInvalidMetadataShape:    "неверные метаданные для бэкенда %s: %s",
		MsgWorkstationRequired:     "к агенту не привязана рабочая станция; передайте workstation_id",
		MsgWorkstationAccessDenied: "агент %s не имеет доступа к рабочей станции %s",
		MsgBackendNotReady:         "бэкенд рабочей станции не готов: %s",

		// Webhooks
		MsgWebhookAuthFailed:                  "аутентификация вебхука не удалась",
		MsgWebhookHMACInvalid:                 "подпись HMAC недействительна",
		MsgWebhookHMACTimestampSkew:           "временная метка запроса вне допустимого окна",
		MsgWebhookBearerRequiredHMAC:          "этот вебхук требует аутентификации HMAC",
		MsgWebhookRevoked:                     "вебхук был отозван",
		MsgWebhookKindMismatch:                "тип запроса не соответствует конфигурации вебхука",
		MsgWebhookRateLimited:                 "превышен лимит запросов вебхука",
		MsgWebhookBodyTooLarge:                "тело запроса превышает ограничение по размеру",
		MsgWebhookIdempotencyConflict:         "конфликт ключа идемпотентности: несоответствие тела запроса",
		MsgWebhookTenantMismatch:              "несоответствие арендатора вебхука",
		MsgWebhookAgentNotFound:               "агент вебхука не найден",
		MsgWebhookChannelNotFound:             "канал вебхука не найден",
		MsgWebhookMediaSSRFBlocked:            "URL медиа заблокирован политикой SSRF",
		MsgWebhookMediaTooLarge:               "медиафайл превышает ограничение по размеру",
		MsgWebhookMediaMIMEDenied:             "MIME-тип медиа не разрешён",
		MsgWebhookCallbackURLInvalid:          "URL обратного вызова недействителен или заблокирован",
		MsgWebhookLLMTimeout:                  "истекло время обработки LLM",
		MsgWebhookLaneSaturated:               "линия обработки вебхуков заполнена",
		MsgWebhookLocalhostOnlyViolation:      "этот вебхук ограничен вызовами только с localhost",
		MsgWebhookMediaChannelUnsupported:     "канал не поддерживает медиавложения",
		MsgWebhookIPDenied:                    "источник запроса не входит в список разрешённых IP",
		MsgWebhookEncryptionUnavailable:       "ключ шифрования вебхука не настроен; установите GOCLAW_ENCRYPTION_KEY, чтобы включить вебхуки",
		MsgWebhookMessageTestRequiresStandard: "тестирование вебхуков сообщений требует издания Standard",

		// Hooks
		MsgHookInvalidMatcher:          "неверное регулярное выражение matcher: %s",
		MsgHookCommandDisabledStandard: "хуки типа command доступны только в издании Lite",
		MsgHookPromptRequiresMatcher:   "хуки prompt требуют matcher или if_expr (защита от неконтролируемых затрат)",
		MsgHookCircuitBreakerTripped:   "хук автоматически отключён после повторяющихся сбоев",
		MsgHookBudgetExceeded:          "превышен бюджет токенов хуков арендатора",
		MsgHookPerTurnCapReached:       "достигнут лимит вызовов хука за один ход",
		MsgHookBuiltinReadOnly:         "встроенные хуки доступны только для чтения, кроме переключателя включения",

		// Workstation permissions (Phase 6)
		MsgWorkstationCmdDenied:    "команда запрещена политикой рабочей станции: %s",
		MsgWorkstationEnvDenied:    "переменная окружения запрещена политикой: %s",
		MsgWorkstationInputInvalid: "команда содержит недопустимые символы: %s",
		MsgWorkstationRateLimit:    "превышен лимит запросов рабочей станции",
		MsgWorkstationPermNotFound: "запись разрешения не найдена: %s",
		// Workstation activity (Phase 7)
		MsgWorkstationActivityTitle: "Недавняя активность",
		MsgWorkstationActionExec:    "Выполнение",
		MsgWorkstationActionDeny:    "Запрещено",

		// Package updates (Phase 4+5)
		MsgPackageNotInstalled:  "Пакет %s не установлен",
		MsgPackageUpdateLocked:  "Пакет %s обновляется другим запросом",
		MsgReleaseNotFound:      "Релиз %s не найден для %s",
		MsgAssetNotFound:        "Нет совместимого ресурса для %s/%s",
		MsgChecksumMismatch:     "Несоответствие контрольной суммы для %s",
		MsgUpdateSwapFailed:     "Не удалось установить %s; восстановлена предыдущая версия",
		MsgUpdateManifestDesync: "Двоичный файл обновлён, но сохранение манифеста не удалось — требуется ручное восстановление для %s",
		MsgUpdateCacheStale:     "Кэш обновлений устарел; обновите кэш перед применением обновления",

		// Grant env validation
		MsgGrantEnvDeniedKeys:   "недопустимые ключи окружения: %s",
		MsgGrantEnvValueInvalid: "неверное значение окружения: %s",
		MsgGrantEnvTooManyKeys:  "слишком много ключей окружения: максимум 50",
		MsgGrantEnvRevealLimit:  "превышен лимит запросов на раскрытие окружения — попробуйте позже",

		// Git credential adapter
		MsgGitCredHostMismatch:             "сохранённые учётные данные git предназначены для %s, но команда нацелена на %s",
		MsgGitCredNoMatch:                  "для хоста %s не настроены учётные данные git",
		MsgGitCredUnsupportedType:          "тип учётных данных git %q не поддерживается",
		MsgGitCredTokenInvalid:             "сохранённый токен git недействителен или пуст",
		MsgGitCredTokenControlChar:         "сохранённый токен git содержит запрещённые управляющие символы",
		MsgGitCredHostUserinfoRejected:     "URL git со встроенными данными пользователя отклонён как неоднозначный",
		MsgGitCredSSHPassphraseUnsupported: "SSH-ключи с парольной фразой не поддерживаются; удалите парольную фразу командой `ssh-keygen -p` перед сохранением",
		MsgGitCredSSHKeyInvalid:            "приватный SSH-ключ недействителен: %s",
		MsgGitCredHostScopeRequired:        "host_scope требуется для credential_type %s",
		MsgGitCredHostScopeInvalid:         "host_scope %q не является корректным именем хоста",
		MsgGitCredBlobMissingField:         "в блобе учётных данных отсутствует обязательное поле %q",
		MsgGitCredUnsupportedCredType:      "credential_type %q не поддерживается",

		// Message tool cross-target forward notice
		MessageCrossTargetForwarded: "📤 Переслано на %s согласно запросу: %q",

		// Package update source labels
		MsgPackagesUpdatesSourceGithub: "GitHub",
		MsgPackagesUpdatesSourcePip:    "pip",
		MsgPackagesUpdatesSourceNpm:    "npm",
		MsgPackagesUpdatesSourceApk:    "apk",

		// Package update availability messages
		MsgPackagesUpdatesUnavailablePip: "pip не установлен в этой системе",
		MsgPackagesUpdatesUnavailableNpm: "npm не установлен в этой системе",
		MsgPackagesUpdatesUnavailableApk: "apk недоступен в этой системе",

		// Package update failure reasons
		MsgPackagesUpdatesReasonDependencyConflict: "Конфликт зависимостей",
		MsgPackagesUpdatesReasonPermission:         "Доступ запрещён",
		MsgPackagesUpdatesReasonNetwork:            "Ошибка сети",
		MsgPackagesUpdatesReasonNotFound:           "Пакет не найден",
		MsgPackagesUpdatesReasonTargetMissing:      "Версия недоступна",
		MsgPackagesUpdatesReasonExternallyManaged:  "Окружение управляется извне",
		MsgPackagesUpdatesReasonLocked:             "База данных пакетов заблокирована",
		MsgPackagesUpdatesReasonDiskFull:           "Диск заполнен",
		MsgPackagesUpdatesReasonHelperUnavailable:  "Привилегированный помощник недоступен",
	})
}
