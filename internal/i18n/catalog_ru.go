package i18n

func init() {
	register(LocaleRU, map[string]string{
		// Common validation
		MsgRequired:            "%s обязательно",
		MsgInvalidID:           "неверный ID %s",
		MsgNotFound:           "%s не найден: %s",
		MsgAlreadyExists:      "%s уже существует: %s",
		MsgInvalidRequest:      "неверный запрос: %s",
		MsgInvalidJSON:        "неверный JSON",
		MsgUnauthorized:       "не авторизован",
		MsgPermissionDenied:   "доступ запрещён: %s",
		MsgInternalError:      "внутренняя ошибка: %s",
		MsgInvalidSlug:        "%s должен быть валидным slug (только строчные буквы, цифры, дефисы)",
		MsgFailedToList:       "не удалось получить список %s",
		MsgFailedToCreate:     "не удалось создать %s: %s",
		MsgFailedToUpdate:     "не удалось обновить %s: %s",
		MsgFailedToDelete:     "не удалось удалить %s: %s",
		MsgFailedToSave:       "не удалось сохранить %s: %s",
		MsgInvalidUpdates:     "неверные обновления",

		// Agent
		MsgAgentNotFound:       "агент не найден: %s",
		MsgCannotDeleteDefault: "нельзя удалить агента по умолчанию",
		MsgUserCtxRequired:     "требуется контекст пользователя",

		// Chat
		MsgRateLimitExceeded: "превышен лимит скорости — пожалуйста, подождите",
		MsgNoUserMessage:     "сообщение пользователя не найдено",
		MsgUserIDRequired:    "user_id обязателен",
		MsgMsgRequired:       "сообщение обязательно",

		// Channel instances
		MsgInvalidChannelType: "неверный channel_type",
		MsgInstanceNotFound:   "инстанс не найден",

		// Cron
		MsgJobNotFound:     "задача не найдена",
		MsgInvalidCronExpr: "неверное cron-выражение: %s",

		// Config
		MsgConfigHashMismatch: "конфигурация изменилась (несовпадение хэша)",

		// Exec approval
		MsgExecApprovalDisabled: "утверждение exec не включено",

		// Pairing
		MsgSenderChannelRequired: "senderId и channel обязательны",
		MsgCodeRequired:          "код обязателен",
		MsgSenderIDRequired:      "sender_id обязателен",

		// HTTP API
		MsgInvalidAuth:           "неверная аутентификация",
		MsgMsgsRequired:          "messages обязательно",
		MsgUserIDHeader:          "заголовок X-GoClaw-User-Id обязателен",
		MsgFileTooLarge:          "файл слишком большой или неверная multipart-форма",
		MsgMissingFileField:      "отсутствует поле 'file'",
		MsgInvalidFilename:       "неверное имя файла",
		MsgChannelKeyReq:        "channel и key обязательны",
		MsgMethodNotAllowed:      "метод не разрешён",
		MsgStreamingNotSupported: "стриминг не поддерживается",
		MsgOwnerOnly:             "только владелец может %s",
		MsgNoAccess:              "нет доступа к этому %s",
		MsgAlreadySummoning:      "агент уже вызывается",
		MsgSummoningUnavailable:  "вызов недоступен",
		MsgNoDescription:         "у агента нет описания для перевызова",
		MsgInvalidPath:           "неверный путь",

		// Scheduler
		MsgQueueFull:    "очередь сессий заполнена",
		MsgShuttingDown: "шлюз завершает работу, повторите попытку",

		// Provider
		MsgProviderReqFailed: "%s: запрос не удался: %s",

		// Unknown method
		MsgUnknownMethod: "неизвестный метод: %s",

		// Not implemented
		MsgNotImplemented: "%s ещё не реализовано",

		// Agent links
		MsgLinksNotConfigured:    "связи агентов не настроены",
		MsgInvalidDirection:      "направление должно быть outbound, inbound или bidirectional",
		MsgSourceTargetSame:     "исходный и целевой агенты должны быть разными",
		MsgCannotDelegateOpen:   "нельзя делегировать открытым агентам — только предопределённые агенты могут быть целями делегирования",
		MsgNoUpdatesProvided:    "обновления не предоставлены",
		MsgInvalidLinkStatus:    "статус должен быть active или disabled",

		// Teams
		MsgTeamsNotConfigured:    "команды не настроены",
		MsgAgentIsTeamLead:      "агент уже является ведущим команды",
		MsgCannotRemoveTeamLead: "нельзя удалить ведущего команды",

		// Channels
		MsgCannotDeleteDefaultInst: "нельзя удалить инстанс канала по умолчанию",
		MsgCannotRemoveLastWriter:  "нельзя удалить последнего редактора файлов",

		// Skills
		MsgSkillsUpdateNotSupported: "skills.update не поддерживается для файловых скиллов",
		MsgCannotResolveSkillID:     "не удалось разрешить ID скилла для файлового скилла",

		// Logs
		MsgInvalidLogAction: "действие должно быть 'start' или 'stop'",

		// Config
		MsgRawConfigRequired: "raw config обязательно",
		MsgRawPatchRequired:  "raw patch обязательно",

		// Storage / File
		MsgCannotDeleteSkillsDir: "нельзя удалить директории скиллов",
		MsgFailedToReadFile:      "не удалось прочитать файл",
		MsgFileNotFound:          "файл не найден",
		MsgInvalidVersion:        "неверная версия",
		MsgVersionNotFound:       "версия не найдена",
		MsgFailedToDeleteFile:    "не удалось удалить",

		// OAuth
		MsgNoPendingOAuth:    "нет ожидающего OAuth-потока",
		MsgFailedToSaveToken: "не удалось сохранить токен",

		// Intent Classify
		MsgStatusWorking:       "🔄 Я работаю над вашим запросом... Пожалуйста, подождите.",
		MsgStatusDetailed:      "🔄 В данный момент я работаю над вашим запросом...\n%s (итерация %d)\nВыполняется: %s\n\nПожалуйста, подождите — я отвечу, когда закончу.",
		MsgStatusPhaseThinking: "Этап: Думаю...",
		MsgStatusPhaseToolExec: "Этап: Выполняю %s",
		MsgStatusPhaseTools:    "Этап: Выполняю инструменты...",
		MsgStatusPhaseCompact:  "Этап: Сжатие контекста...",
		MsgStatusPhaseDefault:  "Этап: Обработка...",
		MsgCancelledReply:      "✋ Отменено. Что бы вы хотели сделать дальше?",
		MsgInjectedAck:         "Понял, я учту это в своей работе.",

		// Knowledge Graph
		MsgEntityIDRequired:        "entity_id обязательно",
		MsgEntityFieldsRequired:    "external_id, name и entity_type обязательны",
		MsgTextRequired:            "text обязательно",
		MsgProviderModelRequired:   "provider и model обязательны",
		MsgInvalidProviderOrModel: "неверный провайдер или модель",

		// Builtin tool descriptions
		MsgToolReadFile:        "Читать содержимое файла из рабочего пространства агента по пути",
		MsgToolWriteFile:       "Писать контент в файл рабочего пространства, создавая директории при необходимости",
		MsgToolListFiles:       "Список файлов и директорий по заданному пути в рабочем пространстве",
		MsgToolEdit:            "Применить целевое редактирование поиск-замена в существующих файлах",
		MsgToolExec:            "Выполнить shell-команду в рабочем пространстве и вернуть stdout/stderr",
		MsgToolWebSearch:        "Искать информацию в вебе через поисковую систему (Brave или DuckDuckGo)",
		MsgToolWebFetch:        "Загрузить веб-страницу или API-точку и извлечь текстовый контент",
		MsgToolMemorySearch:     "Искать в долгосрочной памяти агента через семантическую близость",
		MsgToolMemoryGet:       "Получить конкретный документ памяти по пути к файлу",
		MsgToolKGSearch:        "Искать сущности, связи и наблюдения в базе знаний агента",
		MsgToolReadImage:       "Анализировать изображения с помощью LLM провайдера с поддержкой vision",
		MsgToolReadDocument:    "Анализировать документы (PDF, Word, Excel, PowerPoint, CSV и т.д.) с помощью LLM провайдера",
		MsgToolCreateImage:     "Генерировать изображения из текстовых промптов через провайдера генерации изображений",
		MsgToolReadAudio:       "Анализировать аудиофайлы (речь, музыка, звуки) через LLM провайдер",
		MsgToolReadVideo:       "Анализировать видеофайлы через LLM провайдер",
		MsgToolCreateVideo:     "Генерировать видео из текстовых описаний через AI",
		MsgToolCreateAudio:     "Генерировать музыку или звуки из текстовых описаний через AI",
		MsgToolTTS:             "Конвертировать текст в естественную речь",
		MsgToolBrowser:          "Автоматизировать взаимодействия с браузером: навигация, клики, заполнение форм, скриншоты",
		MsgToolSessionsList:     "Список активных чат-сессий во всех каналах",
		MsgToolSessionStatus:    "Получить текущий статус и метаданные конкретной чат-сессии",
		MsgToolSessionsHistory:  "Получить историю сообщений конкретной чат-сессии",
		MsgToolSessionsSend:     "Отправить сообщение в активную чат-сессию от имени агента",
		MsgToolMessage:          "Отправить проактивное сообщение пользователю в подключённом канале (Telegram, Discord и т.д.)",
		MsgToolCron:             "Планировать или управлять повторяющимися задачами через cron-выражения",
		MsgToolSpawn:            "Породить субагента для выполнения задачи в фоне или делегировать задачу связанному агенту",
		MsgToolSkillSearch:      "Искать доступные скиллы по ключевым словам или описанию",
		MsgToolUseSkill:         "Активировать скилл для использования его специализированных возможностей",
		MsgToolSkillManage:      "Создавать, обновлять или удалять скиллы на основе опыта из разговоров",
		MsgToolPublishSkill:     "Зарегистрировать директорию скилла в системной базе данных для обнаружения",
		MsgToolTeamTasks:        "Просмотр, создание, обновление и завершение задач на доске команды",

		MsgSkillNudgePostscript: "Эта задача включала несколько шагов. Хотите сохранить процесс как скилл? Ответьте **\"сохранить как скилл\"** или **\"пропустить\"**.",
		MsgSkillNudge70Pct:      "[Система] Вы использовали 70% бюджета итераций. Подумайте, можно ли сохранить какие-либо паттерны этого сеанса как скилл.",
		MsgSkillNudge90Pct:      "[Система] Вы использовали 90% бюджета итераций. Если этот сеанс включал повторяемые паттерны, сохраните их как скилл перед завершением.",

		MsgInvalidRole: "неверная роль: допустимые значения: owner, admin, operator, member, viewer",

		MsgContactIDsRequired:   "contact_ids обязательно",
		MsgMergeTargetRequired: "требуется ровно один из tenant_user_id или create_user",
		MsgTenantUserNotFound: "пользователь тенанта не найден",
		MsgTenantMismatch:      "пользователь тенанта не принадлежит этому тенанту",
		MsgTenantScopeRequired: "область тенанта обязательна для этой операции",
	})
}
