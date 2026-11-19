package i18n

func init() {
	register(LocaleES, map[string]string{
		// Common validation
		MsgRequired:         "%s es obligatorio",
		MsgInvalidID:        "ID %s no válido",
		MsgNotFound:         "%s no encontrado: %s",
		MsgAlreadyExists:    "%s ya existe: %s",
		MsgInvalidRequest:   "solicitud no válida: %s",
		MsgInvalidJSON:      "JSON no válido",
		MsgUnauthorized:     "no autorizado",
		MsgPermissionDenied: "permiso denegado: %s",
		MsgInternalError:    "error interno: %s",
		MsgInvalidSlug:      "%s debe ser un slug válido (solo letras minúsculas, números y guiones)",
		MsgFailedToList:     "no se pudo listar %s",
		MsgFailedToCreate:   "no se pudo crear %s: %s",
		MsgFailedToUpdate:   "no se pudo actualizar %s: %s",
		MsgFailedToDelete:   "no se pudo eliminar %s: %s",
		MsgFailedToSave:     "no se pudo guardar %s: %s",
		MsgInvalidUpdates:   "actualizaciones no válidas",
		// Vault orphan collision on agent delete (#1550)
		MsgAgentDeleteVaultConflict: "no se pudo eliminar el agente: un documento de bóveda de este agente está en una ruta que otro propietario eliminado ya dejó huérfana; elimina o cambia la ruta de los documentos de bóveda de este agente primero, o actualiza a la versión de esquema 99 o posterior",

		// Agent
		MsgAgentNotFound:                       "agente no encontrado: %s",
		MsgCannotDeleteDefault:                 "no se puede eliminar el agente predeterminado",
		MsgUserCtxRequired:                     "se requiere contexto de usuario",
		MsgGatewayOperatorSecureCLIUnavailable: "Se omitió el acceso de operador del gateway porque el almacenamiento de SecureCLI no está disponible.",
		MsgGatewayOperatorEligibilityFailed:    "El agente se creó, pero el acceso de operador del gateway no pudo verificar la elegibilidad como primer agente.",
		MsgGatewayOperatorNotFirstAgent:        "No se otorgó el acceso de operador del gateway porque este no es el primer agente.",
		MsgGatewayOperatorTokenMissing:         "Se omitió el acceso de operador del gateway porque el token del gateway no está configurado.",
		MsgGatewayOperatorBinaryMissing:        "Se omitió el acceso de operador del gateway porque no se pudo encontrar el binario goclaw.",
		MsgGatewayOperatorExistingReview:       "Se omitió el acceso de operador del gateway porque una credencial de la CLI de goclaw existente requiere revisión manual.",
		MsgGatewayOperatorRegisterFailed:       "Se omitió el acceso de operador del gateway porque no se pudo registrar la credencial de la CLI de goclaw.",
		MsgGatewayOperatorCredentialFailed:     "Se omitió el acceso de operador del gateway porque no se pudieron guardar las credenciales.",

		// Chat
		MsgRateLimitExceeded: "se superó el límite de velocidad — espera",
		MsgNoUserMessage:     "no se encontró mensaje del usuario",
		MsgUserIDRequired:    "se requiere user_id",
		MsgMsgRequired:       "se requiere un mensaje",

		// Abort
		MsgAbortStopped:         "ejecución detenida",
		MsgAbortForced:          "ejecución abortada por la fuerza (se superó la tolerancia de 3 s)",
		MsgAbortAlreadyAborting: "ya hay una cancelación en curso",
		MsgAbortNotFound:        "ejecución no encontrada o ya finalizada",
		MsgAbortUnauthorized:    "no autorizado para abortar esta ejecución",
		MsgAbortFailed:          "no se pudo abortar la ejecución: %s",

		// Channel instances
		MsgInvalidChannelType: "channel_type no válido",
		MsgInstanceNotFound:   "instancia no encontrada",

		// Cron
		MsgJobNotFound:         "tarea no encontrada",
		MsgInvalidCronExpr:     "expresión cron no válida: %s",
		MsgCommandCronDisabled: "las tareas cron de comando están deshabilitadas en este gateway (define cron.command_enabled=true para permitirlas)",

		// Config
		MsgConfigHashMismatch: "la configuración cambió (el hash no coincide)",

		// Exec approval
		MsgExecApprovalDisabled: "la aprobación de ejecución no está habilitada",

		// Pairing
		MsgSenderChannelRequired: "se requieren senderId y channel",
		MsgCodeRequired:          "se requiere el código",
		MsgSenderIDRequired:      "se requiere sender_id",

		// HTTP API
		MsgInvalidAuth:            "autenticación no válida",
		MsgMsgsRequired:           "se requiere messages",
		MsgUserIDHeader:           "se requiere el encabezado X-GoClaw-User-Id",
		MsgFileTooLarge:           "archivo demasiado grande o formulario multipart no válido",
		MsgMissingFileField:       "falta el campo 'file'",
		MsgInvalidFilename:        "nombre de archivo no válido",
		MsgChannelKeyReq:          "se requieren channel y key",
		MsgMethodNotAllowed:       "método no permitido",
		MsgStreamingNotSupported:  "streaming no compatible",
		MsgOwnerOnly:              "solo el propietario puede %s",
		MsgNoAccess:               "sin acceso a este %s",
		MsgAlreadySummoning:       "el agente ya se está invocando",
		MsgSummoningUnavailable:   "invocación no disponible",
		MsgRunTimelineUnavailable: "cronología de la ejecución no disponible",
		MsgNoDescription:          "el agente no tiene descripción para volver a invocarlo",
		MsgSummonCancelled:        "invocación cancelada por el usuario",
		MsgCannotCancel:           "el agente no se está invocando",
		MsgInvalidPath:            "ruta no válida",

		// Browser cookies
		MsgBrowserCookieTooMany:            "demasiadas cookies del navegador en una sola solicitud de sincronización",
		MsgInvalidCookieURL:                "URL de cookie no válida",
		MsgBrowserCookieValueTooLarge:      "valor de cookie demasiado grande",
		MsgBrowserCookieEncryptionRequired: "el cifrado de cookies del navegador no está configurado",

		// Tenant backup / restore
		MsgRestoreNewModeRejectsTenantID: "mode=new crea un tenant nuevo; pasa tenant_slug (no tenant_id) como slug de destino del nuevo tenant",

		// Scheduler
		MsgQueueFull:    "la cola de sesiones está llena",
		MsgShuttingDown: "el gateway se está apagando; vuelve a intentarlo en breve",

		// Provider
		MsgProviderReqFailed: "%s: la solicitud falló: %s",

		// Usage caps / pricing
		MsgUsageCapsListPoliciesFailed:          "no se pudieron listar las políticas de límites de uso",
		MsgUsageCapPolicyValidationFailed:       "la validación de la política de límites de uso falló",
		MsgUsageCapPolicyManaged:                "las políticas de límites de uso administradas no se pueden modificar",
		MsgUsageCapsDeletePolicyFailed:          "no se pudo eliminar la política de límites de uso",
		MsgUsageCapsUtilizationFailed:           "no se pudo cargar el uso de los límites",
		MsgUsageCapsEventsFailed:                "no se pudieron cargar los eventos de límites de uso",
		MsgUsagePricingSyncOpenRouterFailed:     "no se pudieron sincronizar los precios de OpenRouter: %s",
		MsgUsagePricingStoreCatalogFailed:       "no se pudo guardar el catálogo de precios",
		MsgUsagePricingListFailed:               "no se pudieron listar los precios de modelos",
		MsgUsagePricingProviderModelRequired:    "se requieren provider_id y model_id",
		MsgUsagePricingOverrideValidationFailed: "la validación de la anulación de precios falló",
		MsgUsagePricingListOverridesFailed:      "no se pudieron listar las anulaciones de precios",
		MsgUsagePricingDeleteOverrideFailed:     "no se pudo eliminar la anulación de precios",

		// Unknown method
		MsgUnknownMethod: "método desconocido: %s",

		// Not implemented
		MsgNotImplemented: "%s aún no está implementado",

		// Agent links
		MsgLinksNotConfigured: "los vínculos entre agentes no están configurados",
		MsgInvalidDirection:   "la dirección debe ser outbound, inbound o bidirectional",
		MsgSourceTargetSame:   "origen y destino deben ser agentes diferentes",
		MsgCannotDelegateOpen: "no se puede delegar a agentes abiertos — solo los agentes predefinidos pueden ser destino de delegación",
		MsgNoUpdatesProvided:  "no se proporcionaron actualizaciones",
		MsgInvalidLinkStatus:  "el estado debe ser active o disabled",

		// Teams
		MsgTeamsNotConfigured:   "los equipos no están configurados",
		MsgAgentIsTeamLead:      "el agente ya es el líder del equipo",
		MsgCannotRemoveTeamLead: "no se puede quitar al líder del equipo",

		// Channels
		MsgCannotDeleteDefaultInst: "no se puede eliminar la instancia de canal predeterminada",
		MsgCannotRemoveLastWriter:  "no se puede quitar al último escritor de archivos",

		// Skills
		MsgSkillsUpdateNotSupported:    "skills.update no es compatible con skills basados en archivos",
		MsgCannotResolveSkillID:        "no se puede resolver el ID del skill basado en archivos",
		MsgInvalidVisibility:           "visibilidad %q no válida: debe ser private o public",
		MsgSkillEvolutionNotConfigured: "el almacén de evolución de skills no está configurado",
		MsgActivityStoreNotConfigured:  "el almacén de actividad no está configurado",
		MsgInvalidEvolutionMode:        "modo de evolución no válido",
		MsgSystemSkillMutationBlocked:  "la modificación de skills del sistema está bloqueada",
		MsgSuggestionMustBeApproved:    "la sugerencia debe aprobarse antes de aplicarla",
		MsgInvalidDraftPatch:           "draft_patch no válido: %s",
		MsgDraftPatchRequired:          "draft_patch requiere content o find/replace",
		MsgFindTextNotFound:            "no se encontró el texto de búsqueda en el archivo de destino",

		// Logs
		MsgInvalidLogAction: "la acción debe ser 'start' o 'stop'",

		// Config
		MsgRawConfigRequired:     "se requiere la configuración raw",
		MsgRawPatchRequired:      "se requiere el parche raw",
		MsgConfigMasterScopeOnly: "los métodos config.* son exclusivos del ámbito maestro; usa los endpoints de configuración de herramientas del tenant para anulaciones por tenant",
		MsgMasterScopeRequired:   "esta acción requiere el ámbito del tenant maestro",

		// Storage / File
		MsgCannotDeleteSkillsDir: "no se pueden eliminar los directorios de skills",
		MsgFailedToReadFile:      "no se pudo leer el archivo",
		MsgFileNotFound:          "archivo no encontrado",
		MsgInvalidVersion:        "versión no válida",
		MsgVersionNotFound:       "versión no encontrada",
		MsgFailedToDeleteFile:    "no se pudo eliminar",

		// OAuth
		MsgNoPendingOAuth:       "no hay flujo OAuth pendiente",
		MsgFailedToSaveToken:    "no se pudo guardar el token",
		MsgOAuthCallbackSuccess: "Autorización exitosa. Puedes cerrar esta ventana.",
		MsgOAuthCallbackFailed:  "Autorización fallida. Puedes cerrar esta ventana.",

		// Intent Classify
		MsgStatusWorking:       "🔄 Estoy trabajando en tu solicitud... Espera.",
		MsgStatusDetailed:      "🔄 Estoy trabajando en tu solicitud...\n%s (iteración %d)\nTiempo en ejecución: %s\n\nEspera — te responderé cuando termine.",
		MsgStatusPhaseThinking: "Fase: Pensando...",
		MsgStatusPhaseToolExec: "Fase: Ejecutando %s",
		MsgStatusPhaseTools:    "Fase: Ejecutando herramientas...",
		MsgStatusPhaseCompact:  "Fase: Compactando contexto...",
		MsgStatusPhaseDefault:  "Fase: Procesando...",
		MsgCancelledReply:      "✋ Cancelado. ¿Qué quieres hacer ahora?",
		MsgInjectedAck:         "Entendido, lo incorporaré en lo que estoy trabajando.",

		// Knowledge Graph
		MsgEntityIDRequired:       "se requiere entity_id",
		MsgEntityFieldsRequired:   "se requieren external_id, name y entity_type",
		MsgTextRequired:           "se requiere el texto",
		MsgProviderModelRequired:  "se requieren proveedor y modelo",
		MsgInvalidProviderOrModel: "proveedor o modelo no válidos",

		// Builtin tool descriptions
		MsgToolReadFile:           "Leer el contenido de un archivo del espacio de trabajo del agente por ruta",
		MsgToolWriteFile:          "Escribir contenido en un archivo del espacio de trabajo, creando directorios según sea necesario",
		MsgToolListFiles:          "Listar archivos y directorios en una ruta del espacio de trabajo",
		MsgToolEdit:               "Aplicar ediciones puntuales de buscar-y-reemplazar en archivos existentes sin reescribir todo el archivo",
		MsgToolExec:               "Ejecutar un comando de shell en el espacio de trabajo y devolver stdout/stderr",
		MsgToolWebSearch:          "Buscar información en la web con un motor de búsqueda (Brave o DuckDuckGo)",
		MsgToolWebFetch:           "Obtener una página web o un endpoint de API y extraer su contenido de texto",
		MsgToolMemorySearch:       "Buscar en la memoria a largo plazo del agente por similitud semántica",
		MsgToolMemoryGet:          "Recuperar un documento de memoria específico por su ruta de archivo",
		MsgToolKGSearch:           "Buscar entidades, relaciones y observaciones en el grafo de conocimiento del agente",
		MsgToolReadImage:          "Analizar imágenes con un proveedor de LLM con capacidad de visión",
		MsgToolReadDocument:       "Analizar documentos (PDF, Word, Excel, PowerPoint, CSV, etc.) con un proveedor de LLM con capacidad de documentos",
		MsgToolCreateImage:        "Generar imágenes a partir de indicaciones de texto con un proveedor de generación de imágenes",
		MsgToolReadAudio:          "Analizar archivos de audio (voz, música, sonidos) con un proveedor de LLM con capacidad de audio",
		MsgToolReadVideo:          "Analizar archivos de video con un proveedor de LLM con capacidad de video",
		MsgToolCreateVideo:        "Generar videos a partir de descripciones de texto con IA",
		MsgToolCreateAudio:        "Generar música o efectos de sonido a partir de descripciones de texto con IA",
		MsgToolTTS:                "Convertir texto en audio de voz de sonido natural",
		MsgToolBrowser:            "Automatizar interacciones con el navegador: navegar por páginas, hacer clic en elementos, completar formularios, tomar capturas de pantalla",
		MsgToolSessionsList:       "Listar las sesiones de chat activas en todos los canales",
		MsgToolSessionStatus:      "Obtener el estado actual y los metadatos de una sesión de chat específica",
		MsgToolSessionsHistory:    "Recuperar el historial de mensajes de una sesión de chat específica",
		MsgToolSessionsSend:       "Enviar un mensaje a una sesión de chat activa en nombre del agente",
		MsgToolMessage:            "Enviar un mensaje proactivo a un usuario en un canal conectado (Telegram, Discord, etc.)",
		MsgToolCron:               "Programar o administrar tareas recurrentes con expresiones cron, horas específicas o intervalos",
		MsgToolSpawn:              "Generar un subagente para trabajo en segundo plano o delegar una tarea a un agente vinculado",
		MsgToolSkillSearch:        "Buscar skills disponibles por palabra clave o descripción para encontrar capacidades relevantes",
		MsgToolUseSkill:           "Activar un skill para usar sus capacidades especializadas (marcador de traza)",
		MsgToolSkillManage:        "Crear, parchear o eliminar skills a partir de la experiencia de conversación",
		MsgToolPublishSkill:       "Registrar un directorio de skill en la base de datos del sistema para que sea detectable",
		MsgToolTeamTasks:          "Ver, crear, actualizar y completar tareas en el tablero de tareas del equipo",
		MsgToolAnnouncementSingle: "Usaré %s para el siguiente paso.",
		MsgToolAnnouncementMulti:  "Usaré %s para el siguiente paso.",

		MsgSkillNudgePostscript:  "Esta tarea involucró varios pasos. ¿Quieres que guarde el proceso como un skill reutilizable? Responde **\"guardar como skill\"** o **\"omitir\"**.",
		MsgSkillNudge70Pct:       "[Sistema] Estás en el 70% de tu presupuesto de iteraciones. Considera si algún patrón de esta sesión podría ser un buen skill.",
		MsgSkillNudge90Pct:       "[Sistema] Estás en el 90% de tu presupuesto de iteraciones. Si esta sesión involucró patrones reutilizables, considera guardarlos como un skill antes de completar.",
		MsgEmptyReplyFallback:    "⚠️ El agente no pudo generar una respuesta. Nota: algunas acciones de herramientas pueden haberse ejecutado ya — verifica antes de reintentar",
		MsgContextBudgetExceeded: "⚠️ Esta conversación creció demasiado para la ventana de contexto del modelo, así que me detuve antes de responder. Algunas acciones de herramientas de este turno pueden haberse ejecutado ya. Inicia una sesión nueva para continuar.",

		MsgInvalidRole: "rol no válido: los valores permitidos son owner, admin, operator, member, viewer",

		MsgContactIDsRequired:  "se requiere contact_ids",
		MsgMergeTargetRequired: "se requiere exactamente uno de tenant_user_id o create_user",
		MsgTenantUserNotFound:  "usuario del tenant no encontrado",
		MsgTenantMismatch:      "el usuario no pertenece a este tenant",
		MsgTenantScopeRequired: "se requiere el ámbito del tenant para esta operación",

		// TTS / Voices
		MsgTtsUnknownModel:        "modelo tts desconocido: %s",
		MsgVoicesListFailed:       "no se pudieron listar las voces: %s",
		MsgTtsGeminiInvalidVoice:  "voz de Gemini no válida: %s",
		MsgTtsGeminiSpeakerLimit:  "Gemini TTS admite como máximo 2 hablantes",
		MsgTtsGeminiInvalidModel:  "modelo Gemini TTS no válido: %s",
		MsgTtsGeminiTextOnly:      "Gemini se negó a generar audio. Prueba con texto más simple, sin traducción ni comentarios.",
		MsgTtsParamOutOfRange:     "el parámetro TTS %q con valor %v está fuera del rango [%v, %v]",
		MsgTtsParamUnknownKey:     "el proveedor no admite el parámetro TTS %q",
		MsgTtsMiniMaxVoicesFailed: "no se pudieron obtener las voces de MiniMax: %s",

		// STT
		MsgSTTAllProvidersFailed:     "Todos los proveedores STT fallaron",
		MsgSTTLegacyConfigDeprecated: "La configuración STT heredada está obsoleta; migra a builtin_tools[stt]",
		MsgSTTWhatsappPrivacyWarning: "Habilitar STT para WhatsApp rompe el cifrado de extremo a extremo de los mensajes de voz enviados a este agente.",
		MsgVoiceMessageFallback:      "[Mensaje de voz]",

		// Workstation
		MsgWorkstationNotFound:     "estación de trabajo no encontrada: %s",
		MsgWorkstationKeyExists:    "la clave de la estación de trabajo ya está en uso: %s",
		MsgInvalidBackend:          "tipo de backend no válido: %s (debe ser ssh|docker)",
		MsgWorkstationInactive:     "la estación de trabajo está inactiva: %s",
		MsgInvalidMetadataShape:    "metadatos no válidos para el backend %s: %s",
		MsgWorkstationRequired:     "no hay una estación de trabajo vinculada al agente; pasa workstation_id",
		MsgWorkstationAccessDenied: "el agente %s no está autorizado para la estación de trabajo %s",
		MsgBackendNotReady:         "el backend de la estación de trabajo no está listo: %s",

		// Webhooks
		MsgWebhookAuthFailed:                  "la autenticación del webhook falló",
		MsgWebhookHMACInvalid:                 "la firma HMAC no es válida",
		MsgWebhookHMACTimestampSkew:           "la marca de tiempo de la solicitud está fuera de la ventana aceptable",
		MsgWebhookBearerRequiredHMAC:          "este webhook requiere autenticación HMAC",
		MsgWebhookRevoked:                     "el webhook fue revocado",
		MsgWebhookKindMismatch:                "el tipo de solicitud no coincide con la configuración del webhook",
		MsgWebhookRateLimited:                 "se superó el límite de velocidad del webhook",
		MsgWebhookBodyTooLarge:                "el cuerpo de la solicitud supera el límite de tamaño",
		MsgWebhookIdempotencyConflict:         "conflicto de clave de idempotencia: el cuerpo de la solicitud no coincide",
		MsgWebhookTenantMismatch:              "el tenant del webhook no coincide",
		MsgWebhookAgentNotFound:               "agente del webhook no encontrado",
		MsgWebhookChannelNotFound:             "canal del webhook no encontrado",
		MsgWebhookMediaSSRFBlocked:            "la URL del contenido multimedia fue bloqueada por la política SSRF",
		MsgWebhookMediaTooLarge:               "el archivo multimedia supera el límite de tamaño",
		MsgWebhookMediaMIMEDenied:             "el tipo MIME del contenido multimedia no está permitido",
		MsgWebhookCallbackURLInvalid:          "la URL de callback no es válida o está bloqueada",
		MsgWebhookLLMTimeout:                  "se agotó el tiempo de procesamiento del LLM",
		MsgWebhookLaneSaturated:               "el carril de procesamiento de webhooks está saturado",
		MsgWebhookLocalhostOnlyViolation:      "este webhook está restringido a llamadas desde localhost",
		MsgWebhookMediaChannelUnsupported:     "el canal no admite archivos multimedia adjuntos",
		MsgWebhookIPDenied:                    "el origen de la solicitud no está en la lista de IP permitidas",
		MsgWebhookEncryptionUnavailable:       "la clave de cifrado del webhook no está configurada; define GOCLAW_ENCRYPTION_KEY para habilitar los webhooks",
		MsgWebhookMessageTestRequiresStandard: "probar webhooks de mensajes requiere la edición Standard",

		// Hooks
		MsgHookInvalidMatcher:          "expresión regular del matcher no válida: %s",
		MsgHookCommandDisabledStandard: "los hooks de tipo command solo están disponibles en la edición Lite",
		MsgHookPromptRequiresMatcher:   "los hooks prompt requieren un matcher o if_expr (protección contra costos descontrolados)",
		MsgHookCircuitBreakerTripped:   "el hook se deshabilitó automáticamente tras fallos repetidos",
		MsgHookBudgetExceeded:          "se superó el presupuesto de tokens de hooks del tenant",
		MsgHookPerTurnCapReached:       "se alcanzó el límite de invocaciones del hook por turno",
		MsgHookBuiltinReadOnly:         "los hooks integrados son de solo lectura, salvo el interruptor de habilitación",

		// Workstation permissions (Phase 6)
		MsgWorkstationCmdDenied:    "comando denegado por la política de la estación de trabajo: %s",
		MsgWorkstationEnvDenied:    "variable de entorno denegada por la política: %s",
		MsgWorkstationInputInvalid: "el comando contiene caracteres no válidos: %s",
		MsgWorkstationRateLimit:    "se superó el límite de velocidad de la estación de trabajo",
		MsgWorkstationPermNotFound: "entrada de permiso no encontrada: %s",
		// Workstation activity (Phase 7)
		MsgWorkstationActivityTitle: "Actividad reciente",
		MsgWorkstationActionExec:    "Ejecución",
		MsgWorkstationActionDeny:    "Denegado",

		// Package updates (Phase 4+5)
		MsgPackageNotInstalled:  "El paquete %s no está instalado",
		MsgPackageUpdateLocked:  "El paquete %s se está actualizando con otra solicitud",
		MsgReleaseNotFound:      "No se encontró la versión %s para %s",
		MsgAssetNotFound:        "No hay un recurso compatible para %s/%s",
		MsgChecksumMismatch:     "La suma de verificación no coincide para %s",
		MsgUpdateSwapFailed:     "No se pudo instalar %s; se restauró la versión anterior",
		MsgUpdateManifestDesync: "El binario se actualizó, pero no se pudo guardar el manifiesto — se requiere recuperación manual para %s",
		MsgUpdateCacheStale:     "La caché de actualizaciones está obsoleta; actualízala antes de aplicar una actualización",

		// Grant env validation
		MsgGrantEnvDeniedKeys:   "claves de entorno no permitidas: %s",
		MsgGrantEnvValueInvalid: "valor de entorno no válido: %s",
		MsgGrantEnvTooManyKeys:  "demasiadas claves de entorno: máximo 50",
		MsgGrantEnvRevealLimit:  "se superó el límite de velocidad para mostrar variables de entorno — inténtalo más tarde",

		// Git credential adapter
		MsgGitCredHostMismatch:             "la credencial git guardada es para %s, pero el comando apunta a %s",
		MsgGitCredNoMatch:                  "no hay una credencial git configurada para el host %s",
		MsgGitCredUnsupportedType:          "el tipo de credencial git %q no es compatible",
		MsgGitCredTokenInvalid:             "el token git guardado no es válido o está vacío",
		MsgGitCredTokenControlChar:         "el token git guardado contiene caracteres de control prohibidos",
		MsgGitCredHostUserinfoRejected:     "la URL git con datos de usuario incrustados se rechaza por ambigua",
		MsgGitCredSSHPassphraseUnsupported: "las claves SSH protegidas con frase de contraseña no son compatibles; quita la frase con `ssh-keygen -p` antes de guardar",
		MsgGitCredSSHKeyInvalid:            "clave privada SSH no válida: %s",
		MsgGitCredHostScopeRequired:        "se requiere host_scope para credential_type %s",
		MsgGitCredHostScopeInvalid:         "host_scope %q no es un nombre de host válido",
		MsgGitCredBlobMissingField:         "al blob de credenciales le falta el campo obligatorio %q",
		MsgGitCredUnsupportedCredType:      "credential_type %q no es compatible",

		// Message tool cross-target forward notice
		MessageCrossTargetForwarded: "📤 Reenviado a %s según lo solicitado: %q",

		// Package update source labels
		MsgPackagesUpdatesSourceGithub: "GitHub",
		MsgPackagesUpdatesSourcePip:    "pip",
		MsgPackagesUpdatesSourceNpm:    "npm",
		MsgPackagesUpdatesSourceApk:    "apk",

		// Package update availability messages
		MsgPackagesUpdatesUnavailablePip: "pip no está instalado en este sistema",
		MsgPackagesUpdatesUnavailableNpm: "npm no está instalado en este sistema",
		MsgPackagesUpdatesUnavailableApk: "apk no está disponible en este sistema",

		// Package update failure reasons
		MsgPackagesUpdatesReasonDependencyConflict: "Conflicto de dependencias",
		MsgPackagesUpdatesReasonPermission:         "Permiso denegado",
		MsgPackagesUpdatesReasonNetwork:            "Error de red",
		MsgPackagesUpdatesReasonNotFound:           "Paquete no encontrado",
		MsgPackagesUpdatesReasonTargetMissing:      "Versión no disponible",
		MsgPackagesUpdatesReasonExternallyManaged:  "Entorno administrado externamente",
		MsgPackagesUpdatesReasonLocked:             "La base de datos de paquetes está bloqueada",
		MsgPackagesUpdatesReasonDiskFull:           "Disco lleno",
		MsgPackagesUpdatesReasonHelperUnavailable:  "Asistente privilegiado no disponible",
	})
}
