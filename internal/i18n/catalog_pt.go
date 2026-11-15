package i18n

func init() {
	register(LocalePT, map[string]string{
		// Common validation
		MsgRequired:         "%s é obrigatório",
		MsgInvalidID:        "ID de %s inválido",
		MsgNotFound:         "%s não encontrado: %s",
		MsgAlreadyExists:    "%s já existe: %s",
		MsgInvalidRequest:   "requisição inválida: %s",
		MsgInvalidJSON:      "JSON inválido",
		MsgUnauthorized:     "não autorizado",
		MsgPermissionDenied: "permissão negada: %s",
		MsgInternalError:    "erro interno: %s",
		MsgInvalidSlug:      "%s deve ser um slug válido (apenas letras minúsculas, números e hífens)",
		MsgFailedToList:     "falha ao listar %s",
		MsgFailedToCreate:   "falha ao criar %s: %s",
		MsgFailedToUpdate:   "falha ao atualizar %s: %s",
		MsgFailedToDelete:   "falha ao excluir %s: %s",
		MsgFailedToSave:     "falha ao salvar %s: %s",
		MsgInvalidUpdates:   "atualizações inválidas",
		// Vault orphan collision on agent delete (#1550)
		MsgAgentDeleteVaultConflict: "falha ao excluir o agente: um documento do vault deste agente está em um caminho que outro proprietário excluído já deixou órfão; exclua ou mude o caminho dos documentos do vault deste agente primeiro, ou atualize para a versão de schema 99 ou superior",

		// Agent
		MsgAgentNotFound:                       "agente não encontrado: %s",
		MsgCannotDeleteDefault:                 "não é possível excluir o agente padrão",
		MsgUserCtxRequired:                     "contexto de usuário obrigatório",
		MsgGatewayOperatorSecureCLIUnavailable: "O acesso de operador do gateway foi ignorado porque o armazenamento do SecureCLI não está disponível.",
		MsgGatewayOperatorEligibilityFailed:    "O agente foi criado, mas o acesso de operador do gateway não conseguiu verificar a elegibilidade do primeiro agente.",
		MsgGatewayOperatorNotFirstAgent:        "O acesso de operador do gateway não foi concedido porque este não é o primeiro agente.",
		MsgGatewayOperatorTokenMissing:         "O acesso de operador do gateway foi ignorado porque o token do gateway não está configurado.",
		MsgGatewayOperatorBinaryMissing:        "O acesso de operador do gateway foi ignorado porque o binário do goclaw não foi encontrado.",
		MsgGatewayOperatorExistingReview:       "O acesso de operador do gateway foi ignorado porque uma credencial existente da CLI do goclaw exige revisão manual.",
		MsgGatewayOperatorRegisterFailed:       "O acesso de operador do gateway foi ignorado porque a credencial da CLI do goclaw não pôde ser registrada.",
		MsgGatewayOperatorCredentialFailed:     "O acesso de operador do gateway foi ignorado porque as credenciais não puderam ser armazenadas.",

		// Chat
		MsgRateLimitExceeded: "limite de requisições excedido — aguarde",
		MsgNoUserMessage:     "nenhuma mensagem de usuário encontrada",
		MsgUserIDRequired:    "user_id é obrigatório",
		MsgMsgRequired:       "a mensagem é obrigatória",

		// Abort
		MsgAbortStopped:         "execução interrompida",
		MsgAbortForced:          "execução abortada à força (tolerância de 3s excedida)",
		MsgAbortAlreadyAborting: "o cancelamento já está em andamento",
		MsgAbortNotFound:        "execução não encontrada ou já finalizada",
		MsgAbortUnauthorized:    "sem autorização para abortar esta execução",
		MsgAbortFailed:          "falha ao abortar a execução: %s",

		// Channel instances
		MsgInvalidChannelType: "channel_type inválido",
		MsgInstanceNotFound:   "instância não encontrada",

		// Cron
		MsgJobNotFound:         "tarefa não encontrada",
		MsgInvalidCronExpr:     "expressão cron inválida: %s",
		MsgCommandCronDisabled: "tarefas cron de comando estão desativadas neste gateway (defina cron.command_enabled=true para permiti-las)",

		// Config
		MsgConfigHashMismatch: "a configuração foi alterada (hash divergente)",

		// Exec approval
		MsgExecApprovalDisabled: "a aprovação de execução não está ativada",

		// Pairing
		MsgSenderChannelRequired: "senderId e channel são obrigatórios",
		MsgCodeRequired:          "o código é obrigatório",
		MsgSenderIDRequired:      "sender_id é obrigatório",

		// HTTP API
		MsgInvalidAuth:            "autenticação inválida",
		MsgMsgsRequired:           "messages é obrigatório",
		MsgUserIDHeader:           "o cabeçalho X-GoClaw-User-Id é obrigatório",
		MsgFileTooLarge:           "arquivo muito grande ou formulário multipart inválido",
		MsgMissingFileField:       "campo 'file' ausente",
		MsgInvalidFilename:        "nome de arquivo inválido",
		MsgChannelKeyReq:          "channel e key são obrigatórios",
		MsgMethodNotAllowed:       "método não permitido",
		MsgStreamingNotSupported:  "streaming não suportado",
		MsgOwnerOnly:              "somente o proprietário pode %s",
		MsgNoAccess:               "sem acesso a este(a) %s",
		MsgAlreadySummoning:       "o agente já está sendo invocado",
		MsgSummoningUnavailable:   "invocação indisponível",
		MsgRunTimelineUnavailable: "linha do tempo da execução indisponível",
		MsgNoDescription:          "o agente não tem descrição para reinvocar",
		MsgSummonCancelled:        "invocação cancelada pelo usuário",
		MsgCannotCancel:           "o agente não está sendo invocado",
		MsgInvalidPath:            "caminho inválido",

		// Browser cookies
		MsgBrowserCookieTooMany:            "cookies de navegador demais em uma única requisição de sincronização",
		MsgInvalidCookieURL:                "URL de cookie inválida",
		MsgBrowserCookieValueTooLarge:      "valor do cookie muito grande",
		MsgBrowserCookieEncryptionRequired: "a criptografia de cookies do navegador não está configurada",

		// Tenant backup / restore
		MsgRestoreNewModeRejectsTenantID: "mode=new cria um tenant novo; informe tenant_slug (não tenant_id) como slug de destino do novo tenant",

		// Scheduler
		MsgQueueFull:    "a fila de sessões está cheia",
		MsgShuttingDown: "o gateway está sendo desligado, tente novamente em instantes",

		// Provider
		MsgProviderReqFailed: "%s: a requisição falhou: %s",

		// Usage caps / pricing
		MsgUsageCapsListPoliciesFailed:          "falha ao listar as políticas de limite de uso",
		MsgUsageCapPolicyValidationFailed:       "falha na validação da política de limite de uso",
		MsgUsageCapPolicyManaged:                "políticas de limite de uso gerenciadas não podem ser modificadas",
		MsgUsageCapsDeletePolicyFailed:          "falha ao excluir a política de limite de uso",
		MsgUsageCapsUtilizationFailed:           "falha ao carregar a utilização dos limites de uso",
		MsgUsageCapsEventsFailed:                "falha ao carregar os eventos dos limites de uso",
		MsgUsagePricingSyncOpenRouterFailed:     "falha ao sincronizar os preços do OpenRouter: %s",
		MsgUsagePricingStoreCatalogFailed:       "falha ao armazenar o catálogo de preços",
		MsgUsagePricingListFailed:               "falha ao listar os preços dos modelos",
		MsgUsagePricingProviderModelRequired:    "provider_id e model_id são obrigatórios",
		MsgUsagePricingOverrideValidationFailed: "falha na validação da substituição de preço",
		MsgUsagePricingListOverridesFailed:      "falha ao listar as substituições de preço",
		MsgUsagePricingDeleteOverrideFailed:     "falha ao excluir a substituição de preço",

		// Unknown method
		MsgUnknownMethod: "método desconhecido: %s",

		// Not implemented
		MsgNotImplemented: "%s ainda não implementado",

		// Agent links
		MsgLinksNotConfigured: "vínculos de agentes não configurados",
		MsgInvalidDirection:   "a direção deve ser outbound, inbound ou bidirectional",
		MsgSourceTargetSame:   "origem e destino devem ser agentes diferentes",
		MsgCannotDelegateOpen: "não é possível delegar a agentes abertos — somente agentes predefinidos podem ser destinos de delegação",
		MsgNoUpdatesProvided:  "nenhuma atualização informada",
		MsgInvalidLinkStatus:  "o status deve ser active ou disabled",

		// Teams
		MsgTeamsNotConfigured:   "equipes não configuradas",
		MsgAgentIsTeamLead:      "o agente já é o líder da equipe",
		MsgCannotRemoveTeamLead: "não é possível remover o líder da equipe",

		// Channels
		MsgCannotDeleteDefaultInst: "não é possível excluir a instância de canal padrão",
		MsgCannotRemoveLastWriter:  "não é possível remover o último gravador de arquivos",

		// Skills
		MsgSkillsUpdateNotSupported:    "skills.update não é suportado para skills baseadas em arquivo",
		MsgCannotResolveSkillID:        "não foi possível resolver o ID da skill baseada em arquivo",
		MsgInvalidVisibility:           "visibilidade %q inválida: deve ser private ou public",
		MsgSkillEvolutionNotConfigured: "o armazenamento de evolução de skills não está configurado",
		MsgActivityStoreNotConfigured:  "o armazenamento de atividades não está configurado",
		MsgInvalidEvolutionMode:        "modo de evolução inválido",
		MsgSystemSkillMutationBlocked:  "a modificação de skills do sistema está bloqueada",
		MsgSuggestionMustBeApproved:    "a sugestão deve ser aprovada antes de ser aplicada",
		MsgInvalidDraftPatch:           "draft_patch inválido: %s",
		MsgDraftPatchRequired:          "draft_patch requer conteúdo ou localizar/substituir",
		MsgFindTextNotFound:            "texto a localizar não encontrado no arquivo de destino",

		// Logs
		MsgInvalidLogAction: "a ação deve ser 'start' ou 'stop'",

		// Config
		MsgRawConfigRequired:     "a configuração bruta é obrigatória",
		MsgRawPatchRequired:      "o patch bruto é obrigatório",
		MsgConfigMasterScopeOnly: "os métodos config.* são exclusivos do escopo master; use os endpoints de configuração de ferramentas do tenant para substituições por tenant",
		MsgMasterScopeRequired:   "esta ação requer o escopo de tenant master",

		// Storage / File
		MsgCannotDeleteSkillsDir: "não é possível excluir os diretórios de skills",
		MsgFailedToReadFile:      "falha ao ler o arquivo",
		MsgFileNotFound:          "arquivo não encontrado",
		MsgInvalidVersion:        "versão inválida",
		MsgVersionNotFound:       "versão não encontrada",
		MsgFailedToDeleteFile:    "falha ao excluir",

		// OAuth
		MsgNoPendingOAuth:       "nenhum fluxo OAuth pendente",
		MsgFailedToSaveToken:    "falha ao salvar o token",
		MsgOAuthCallbackSuccess: "Autorização concluída com sucesso. Você pode fechar esta janela.",
		MsgOAuthCallbackFailed:  "Falha na autorização. Você pode fechar esta janela.",

		// Intent Classify
		MsgStatusWorking:       "🔄 Estou trabalhando na sua solicitação... Aguarde.",
		MsgStatusDetailed:      "🔄 Estou trabalhando na sua solicitação...\n%s (iteração %d)\nEm execução há: %s\n\nAguarde — responderei quando terminar.",
		MsgStatusPhaseThinking: "Fase: Pensando...",
		MsgStatusPhaseToolExec: "Fase: Executando %s",
		MsgStatusPhaseTools:    "Fase: Executando ferramentas...",
		MsgStatusPhaseCompact:  "Fase: Compactando o contexto...",
		MsgStatusPhaseDefault:  "Fase: Processando...",
		MsgCancelledReply:      "✋ Cancelado. O que você gostaria de fazer em seguida?",
		MsgInjectedAck:         "Entendi, vou incorporar isso ao que estou fazendo.",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_id é obrigatório",
		MsgEntityFieldsRequired:   "external_id, name e entity_type são obrigatórios",
		MsgTextRequired:           "o texto é obrigatório",
		MsgProviderModelRequired:  "provider e model são obrigatórios",
		MsgInvalidProviderOrModel: "provider ou modelo inválido",

		// Builtin tool descriptions
		MsgToolReadFile:           "Ler o conteúdo de um arquivo do workspace do agente pelo caminho",
		MsgToolWriteFile:          "Gravar conteúdo em um arquivo do workspace, criando os diretórios necessários",
		MsgToolListFiles:          "Listar arquivos e diretórios em um caminho do workspace",
		MsgToolEdit:               "Aplicar edições pontuais de localizar e substituir em arquivos existentes sem reescrever o arquivo inteiro",
		MsgToolExec:               "Executar um comando de shell no workspace e retornar stdout/stderr",
		MsgToolWebSearch:          "Pesquisar informações na web usando um mecanismo de busca (Brave ou DuckDuckGo)",
		MsgToolWebFetch:           "Buscar uma página web ou endpoint de API e extrair o conteúdo em texto",
		MsgToolMemorySearch:       "Pesquisar na memória de longo prazo do agente por similaridade semântica",
		MsgToolMemoryGet:          "Recuperar um documento de memória específico pelo caminho do arquivo",
		MsgToolKGSearch:           "Pesquisar entidades, relações e observações no grafo de conhecimento do agente",
		MsgToolReadImage:          "Analisar imagens usando um provider de LLM com capacidade de visão",
		MsgToolReadDocument:       "Analisar documentos (PDF, Word, Excel, PowerPoint, CSV etc.) usando um provider de LLM com suporte a documentos",
		MsgToolCreateImage:        "Gerar imagens a partir de prompts de texto usando um provider de geração de imagens",
		MsgToolReadAudio:          "Analisar arquivos de áudio (fala, música, sons) usando um provider de LLM com suporte a áudio",
		MsgToolReadVideo:          "Analisar arquivos de vídeo usando um provider de LLM com suporte a vídeo",
		MsgToolCreateVideo:        "Gerar vídeos a partir de descrições em texto usando IA",
		MsgToolCreateAudio:        "Gerar músicas ou efeitos sonoros a partir de descrições em texto usando IA",
		MsgToolTTS:                "Converter texto em áudio de fala com som natural",
		MsgToolBrowser:            "Automatizar interações no navegador: navegar por páginas, clicar em elementos, preencher formulários, tirar capturas de tela",
		MsgToolSessionsList:       "Listar as sessões de chat ativas em todos os canais",
		MsgToolSessionStatus:      "Obter o status atual e os metadados de uma sessão de chat específica",
		MsgToolSessionsHistory:    "Recuperar o histórico de mensagens de uma sessão de chat específica",
		MsgToolSessionsSend:       "Enviar uma mensagem a uma sessão de chat ativa em nome do agente",
		MsgToolMessage:            "Enviar uma mensagem proativa a um usuário em um canal conectado (Telegram, Discord etc.)",
		MsgToolCron:               "Agendar ou gerenciar tarefas recorrentes usando expressões cron, horários únicos ou intervalos",
		MsgToolSpawn:              "Iniciar um subagente para trabalho em segundo plano ou delegar uma tarefa a um agente vinculado",
		MsgToolSkillSearch:        "Pesquisar skills disponíveis por palavra-chave ou descrição para encontrar as capacidades relevantes",
		MsgToolUseSkill:           "Ativar uma skill para usar suas capacidades especializadas (marcador de rastreamento)",
		MsgToolSkillManage:        "Criar, aplicar patch ou excluir skills a partir da experiência da conversa",
		MsgToolPublishSkill:       "Registrar um diretório de skill no banco de dados do sistema, tornando-a detectável",
		MsgToolTeamTasks:          "Ver, criar, atualizar e concluir tarefas no quadro de tarefas da equipe",
		MsgToolAnnouncementSingle: "Vou usar %s para cuidar da próxima etapa.",
		MsgToolAnnouncementMulti:  "Vou usar %s para cuidar da próxima etapa.",

		MsgSkillNudgePostscript:  "Esta tarefa envolveu várias etapas. Quer que eu salve o processo como uma skill reutilizável? Responda **\"salvar como skill\"** ou **\"pular\"**.",
		MsgSkillNudge70Pct:       "[Sistema] Você está em 70% do seu orçamento de iterações. Avalie se algum padrão desta sessão daria uma boa skill.",
		MsgSkillNudge90Pct:       "[Sistema] Você está em 90% do seu orçamento de iterações. Se esta sessão envolveu padrões reutilizáveis, considere salvá-los como uma skill antes de concluir.",
		MsgEmptyReplyFallback:    "⚠️ O agente não conseguiu gerar uma resposta. Observação: algumas ações de ferramentas podem já ter sido executadas — verifique antes de tentar novamente",
		MsgContextBudgetExceeded: "⚠️ Esta conversa ficou longa demais para a janela de contexto do modelo, então parei antes de responder. Algumas ações de ferramentas deste turno podem já ter sido executadas. Inicie uma nova sessão para continuar.",

		MsgInvalidRole: "papel inválido: os valores permitidos são owner, admin, operator, member, viewer",

		MsgContactIDsRequired:  "contact_ids é obrigatório",
		MsgMergeTargetRequired: "exatamente um entre tenant_user_id ou create_user é obrigatório",
		MsgTenantUserNotFound:  "usuário do tenant não encontrado",
		MsgTenantMismatch:      "o usuário do tenant não pertence a este tenant",
		MsgTenantScopeRequired: "o escopo de tenant é obrigatório para esta operação",

		// TTS / Voices
		MsgTtsUnknownModel:        "modelo de tts desconhecido: %s",
		MsgVoicesListFailed:       "falha ao listar as vozes: %s",
		MsgTtsGeminiInvalidVoice:  "voz do Gemini inválida: %s",
		MsgTtsGeminiSpeakerLimit:  "o TTS do Gemini suporta no máximo 2 falantes",
		MsgTtsGeminiInvalidModel:  "modelo de TTS do Gemini inválido: %s",
		MsgTtsGeminiTextOnly:      "O Gemini se recusou a gerar áudio. Tente um texto mais simples, sem tradução ou comentários.",
		MsgTtsParamOutOfRange:     "o parâmetro de TTS %q com valor %v está fora do intervalo [%v, %v]",
		MsgTtsParamUnknownKey:     "o parâmetro de TTS %q não é suportado por este provider",
		MsgTtsMiniMaxVoicesFailed: "falha ao buscar as vozes do MiniMax: %s",

		// STT
		MsgSTTAllProvidersFailed:     "Todos os providers de STT falharam",
		MsgSTTLegacyConfigDeprecated: "A configuração legada de STT está obsoleta; migre para builtin_tools[stt]",
		MsgSTTWhatsappPrivacyWarning: "Ativar o STT no WhatsApp quebra a criptografia de ponta a ponta das mensagens de voz enviadas a este agente.",
		MsgVoiceMessageFallback:      "[Mensagem de voz]",

		// Workstation
		MsgWorkstationNotFound:     "workstation não encontrada: %s",
		MsgWorkstationKeyExists:    "chave de workstation já em uso: %s",
		MsgInvalidBackend:          "tipo de backend inválido: %s (deve ser ssh|docker)",
		MsgWorkstationInactive:     "a workstation está inativa: %s",
		MsgInvalidMetadataShape:    "metadados inválidos para o backend %s: %s",
		MsgWorkstationRequired:     "nenhuma workstation vinculada ao agente; informe workstation_id",
		MsgWorkstationAccessDenied: "o agente %s não está autorizado para a workstation %s",
		MsgBackendNotReady:         "o backend da workstation não está pronto: %s",

		// Webhooks
		MsgWebhookAuthFailed:                  "falha na autenticação do webhook",
		MsgWebhookHMACInvalid:                 "a assinatura HMAC é inválida",
		MsgWebhookHMACTimestampSkew:           "o timestamp da requisição está fora da janela aceitável",
		MsgWebhookBearerRequiredHMAC:          "este webhook exige autenticação HMAC",
		MsgWebhookRevoked:                     "o webhook foi revogado",
		MsgWebhookKindMismatch:                "o tipo da requisição não corresponde à configuração do webhook",
		MsgWebhookRateLimited:                 "limite de requisições do webhook excedido",
		MsgWebhookBodyTooLarge:                "o corpo da requisição excede o limite de tamanho",
		MsgWebhookIdempotencyConflict:         "conflito de chave de idempotência: o corpo da requisição não confere",
		MsgWebhookTenantMismatch:              "tenant do webhook divergente",
		MsgWebhookAgentNotFound:               "agente do webhook não encontrado",
		MsgWebhookChannelNotFound:             "canal do webhook não encontrado",
		MsgWebhookMediaSSRFBlocked:            "URL de mídia bloqueada pela política de SSRF",
		MsgWebhookMediaTooLarge:               "o arquivo de mídia excede o limite de tamanho",
		MsgWebhookMediaMIMEDenied:             "o tipo MIME da mídia não é permitido",
		MsgWebhookCallbackURLInvalid:          "a URL de callback é inválida ou está bloqueada",
		MsgWebhookLLMTimeout:                  "o processamento do LLM excedeu o tempo limite",
		MsgWebhookLaneSaturated:               "a fila de processamento de webhooks está na capacidade máxima",
		MsgWebhookLocalhostOnlyViolation:      "este webhook é restrito a chamadas de localhost",
		MsgWebhookMediaChannelUnsupported:     "o canal não suporta anexos de mídia",
		MsgWebhookIPDenied:                    "a origem da requisição não está na lista de IPs permitidos",
		MsgWebhookEncryptionUnavailable:       "a chave de criptografia de webhooks não está configurada; defina GOCLAW_ENCRYPTION_KEY para ativar os webhooks",
		MsgWebhookMessageTestRequiresStandard: "testar webhooks de mensagem requer a edição Standard",

		// Hooks
		MsgHookInvalidMatcher:          "regex de matcher inválida: %s",
		MsgHookCommandDisabledStandard: "hooks do tipo comando só estão disponíveis na edição Lite",
		MsgHookPromptRequiresMatcher:   "hooks de prompt exigem um matcher ou if_expr (proteção contra custo descontrolado)",
		MsgHookCircuitBreakerTripped:   "hook desativado automaticamente após falhas repetidas",
		MsgHookBudgetExceeded:          "orçamento de tokens de hooks do tenant excedido",
		MsgHookPerTurnCapReached:       "limite de invocações de hook por turno atingido",
		MsgHookBuiltinReadOnly:         "hooks nativos são somente leitura, exceto pelo botão de ativação",

		// Workstation permissions (Phase 6)
		MsgWorkstationCmdDenied:    "comando negado pela política da workstation: %s",
		MsgWorkstationEnvDenied:    "variável de ambiente negada pela política: %s",
		MsgWorkstationInputInvalid: "o comando contém caracteres inválidos: %s",
		MsgWorkstationRateLimit:    "limite de requisições da workstation excedido",
		MsgWorkstationPermNotFound: "entrada de permissão não encontrada: %s",
		// Workstation activity (Phase 7)
		MsgWorkstationActivityTitle: "Atividade recente",
		MsgWorkstationActionExec:    "Execução",
		MsgWorkstationActionDeny:    "Negado",

		// Package updates (Phase 4+5)
		MsgPackageNotInstalled:  "O pacote %s não está instalado",
		MsgPackageUpdateLocked:  "O pacote %s está sendo atualizado por outra requisição",
		MsgReleaseNotFound:      "Release %s não encontrada para %s",
		MsgAssetNotFound:        "Nenhum asset compatível para %s/%s",
		MsgChecksumMismatch:     "Divergência de checksum em %s",
		MsgUpdateSwapFailed:     "Falha ao instalar %s; versão anterior restaurada",
		MsgUpdateManifestDesync: "Binário atualizado, mas falhou ao salvar o manifesto — é necessária recuperação manual para %s",
		MsgUpdateCacheStale:     "Cache de atualizações desatualizado; execute a atualização da lista antes de aplicar uma atualização",

		// Grant env validation
		MsgGrantEnvDeniedKeys:   "chaves de ambiente não permitidas: %s",
		MsgGrantEnvValueInvalid: "valor de ambiente inválido: %s",
		MsgGrantEnvTooManyKeys:  "chaves de ambiente demais: máximo de 50",
		MsgGrantEnvRevealLimit:  "limite de requisições excedido para exibir variáveis de ambiente — tente novamente mais tarde",

		// Git credential adapter
		MsgGitCredHostMismatch:             "a credencial git armazenada é para %s, mas o comando tem como alvo %s",
		MsgGitCredNoMatch:                  "nenhuma credencial git configurada para o host %s",
		MsgGitCredUnsupportedType:          "o tipo de credencial git %q não é suportado",
		MsgGitCredTokenInvalid:             "o token git armazenado é inválido ou está vazio",
		MsgGitCredTokenControlChar:         "o token git armazenado contém caracteres de controle proibidos",
		MsgGitCredHostUserinfoRejected:     "URL git com userinfo embutido é rejeitada por ser ambígua",
		MsgGitCredSSHPassphraseUnsupported: "chaves SSH protegidas por senha não são suportadas; remova a senha com `ssh-keygen -p` antes de salvar",
		MsgGitCredSSHKeyInvalid:            "chave privada SSH inválida: %s",
		MsgGitCredHostScopeRequired:        "host_scope é obrigatório para o credential_type %s",
		MsgGitCredHostScopeInvalid:         "host_scope %q não é um nome de host válido",
		MsgGitCredBlobMissingField:         "o blob de credencial não possui o campo obrigatório %q",
		MsgGitCredUnsupportedCredType:      "o credential_type %q não é suportado",

		// Message tool cross-target forward notice
		MessageCrossTargetForwarded: "📤 Encaminhado para %s conforme solicitado: %q",

		// Package update source labels
		MsgPackagesUpdatesSourceGithub: "GitHub",
		MsgPackagesUpdatesSourcePip:    "pip",
		MsgPackagesUpdatesSourceNpm:    "npm",
		MsgPackagesUpdatesSourceApk:    "apk",

		// Package update availability messages
		MsgPackagesUpdatesUnavailablePip: "pip não está instalado neste sistema",
		MsgPackagesUpdatesUnavailableNpm: "npm não está instalado neste sistema",
		MsgPackagesUpdatesUnavailableApk: "apk não está disponível neste sistema",

		// Package update failure reasons
		MsgPackagesUpdatesReasonDependencyConflict: "Conflito de dependências",
		MsgPackagesUpdatesReasonPermission:         "Permissão negada",
		MsgPackagesUpdatesReasonNetwork:            "Erro de rede",
		MsgPackagesUpdatesReasonNotFound:           "Pacote não encontrado",
		MsgPackagesUpdatesReasonTargetMissing:      "Versão não disponível",
		MsgPackagesUpdatesReasonExternallyManaged:  "Ambiente gerenciado externamente",
		MsgPackagesUpdatesReasonLocked:             "O banco de dados de pacotes está bloqueado",
		MsgPackagesUpdatesReasonDiskFull:           "Disco cheio",
		MsgPackagesUpdatesReasonHelperUnavailable:  "Helper privilegiado indisponível",
	})
}
