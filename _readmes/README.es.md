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
  <a href="https://docs.goclaw.sh">Documentación</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Inicio Rápido</a> •
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

**GoClaw** es una pasarela de agentes de IA multi-agente que conecta LLMs a tus herramientas, canales y datos — desplegada como un único binario Go sin dependencias de tiempo de ejecución. Orquesta equipos de agentes y delegación inter-agente entre más de 20 proveedores de LLM con total aislamiento multi-tenant.

Un port en Go de [OpenClaw](https://github.com/openclaw/openclaw) con seguridad mejorada, PostgreSQL multi-tenant y observabilidad de nivel productivo.

🌐 **Idiomas:**
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
## Qué lo Hace Diferente

- **Equipos de Agentes y Orquestación** — Equipos con tableros de tareas compartidos, delegación inter-agente (síncrona/asíncrona) y descubrimiento híbrido de agentes
- **PostgreSQL Multi-Tenant** — Espacios de trabajo por usuario, archivos de contexto por usuario, claves API cifradas (AES-256-GCM), sesiones aisladas
- **Binario Único** — Binario estático Go de ~25 MB, sin tiempo de ejecución Node.js, inicio en <1s, funciona en un VPS de $5
- **Seguridad de Producción** — Sistema de permisos de 5 capas (autenticación de pasarela → política global de herramientas → por agente → por canal → solo propietario) más limitación de velocidad, detección de inyección de prompts, protección SSRF, patrones de denegación de shell y cifrado AES-256-GCM
- **Más de 20 Proveedores de LLM** — Anthropic (HTTP+SSE nativo con caché de prompts), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP y cualquier endpoint compatible con OpenAI
- **7 Canales de Mensajería** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Modo de pensamiento por proveedor (tokens de presupuesto Anthropic, esfuerzo de razonamiento OpenAI, presupuesto de pensamiento DashScope) con soporte de streaming
- **Heartbeat** — Verificaciones periódicas de agentes mediante listas de verificación HEARTBEAT.md con supresión en OK, horas activas, lógica de reintento y entrega por canal
- **Programación y Cron** — Expresiones `at`, `every` y cron para tareas automatizadas de agentes con concurrencia basada en carriles
- **Observabilidad** — Trazado integrado de llamadas LLM con tramos y métricas de caché de prompts, exportación opcional OpenTelemetry OTLP

## Ecosistema Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Lenguaje        | TypeScript      | Rust     | Go       | **Go**                                  |
| Tamaño binario  | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Imagen Docker   | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inactivo)  | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Inicio          | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Hardware objetivo | $599+ Mac Mini | $10 edge | $10 edge | **$5 VPS+**                             |

| Característica                  | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| ------------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)       | —                                    | —                                            | —                                     | ✅                             |
| Integración MCP                 | — (usa ACP)                          | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Equipos de agentes              | —                                    | —                                            | —                                     | ✅ Tablero de tareas + buzón   |
| Seguridad reforzada             | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Básica (workspace restrict, exec deny) | ✅ Defensa de 5 capas          |
| Observabilidad OTel             | ✅ (extensión opt-in)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (build tag opt-in)     |
| Caché de prompts                | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Grafo de conocimiento           | —                                    | —                                            | —                                     | ✅ Extracción LLM + traversal  |
| Sistema de habilidades          | ✅ Embeddings/semántico              | ✅ SKILL.md + TOML                           | ✅ Básico                             | ✅ BM25 + pgvector híbrido     |
| Programador por carriles        | ✅                                   | Concurrencia acotada                         | —                                     | ✅ (main/subagent/team/cron)   |
| Canales de mensajería           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Aplicaciones complementarias   | macOS, iOS, Android                  | Python SDK                                   | —                                     | Panel web                      |
| Live Canvas / Voz               | ✅ (A2UI + TTS/STT)                  | —                                            | Transcripción de voz                  | TTS (4 proveedores)            |
| Proveedores LLM                 | 10+                                  | 8 nativo + 29 compat                         | 13+                                   | **20+**                        |
| Espacios de trabajo por usuario | ✅ (basado en archivos)              | —                                            | —                                     | ✅ (PostgreSQL)                |
| Secretos cifrados               | — (solo vars de entorno)             | ✅ ChaCha20-Poly1305                         | — (JSON en texto plano)               | ✅ AES-256-GCM en BD           |

## Arquitectura

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Inicio Rápido

**Requisitos previos:** Go 1.26+, PostgreSQL 18 con pgvector, Docker (opcional)

### Desde el Código Fuente

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Asistente de configuración interactivo
source .env.local && ./goclaw
```

### Con Docker

```bash
# Generar .env con secretos auto-generados
chmod +x prepare-env.sh && ./prepare-env.sh

# Añade al menos una GOCLAW_*_API_KEY al .env, luego:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Panel web en http://localhost:3000
# Verificación de estado: curl http://localhost:18790/health
```

Cuando las variables de entorno `GOCLAW_*_API_KEY` están configuradas, la pasarela se incorpora automáticamente sin prompts interactivos — detecta el proveedor, ejecuta migraciones e inicializa los datos por defecto.

> Para variantes de compilación (OTel, Tailscale, Redis), etiquetas de imagen Docker y superposiciones de compose, consulta la [Guía de Despliegue](https://docs.goclaw.sh/#deploy-docker-compose).

## Orquestación Multi-Agente

GoClaw admite equipos de agentes y delegación inter-agente — cada agente se ejecuta con su propia identidad, herramientas, proveedor LLM y archivos de contexto.

### Delegación de Agentes

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Modo | Cómo funciona | Ideal para |
|------|---------------|------------|
| **Sync** | El Agente A pregunta al Agente B y **espera** la respuesta | Consultas rápidas, verificación de datos |
| **Async** | El Agente A pregunta al Agente B y **continúa**. B anuncia después | Tareas largas, informes, análisis profundo |

Los agentes se comunican a través de **enlaces de permisos** explícitos con control de dirección (`outbound`, `inbound`, `bidirectional`) y límites de concurrencia tanto a nivel de enlace como de agente.

### Equipos de Agentes

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Tablero de tareas compartido** — Crear, reclamar, completar y buscar tareas con dependencias `blocked_by`
- **Buzón del equipo** — Mensajería directa entre pares y transmisiones
- **Herramientas**: `team_tasks` para gestión de tareas, `team_message` para el buzón

> Para detalles de delegación, enlaces de permisos y control de concurrencia, consulta la [documentación de Equipos de Agentes](https://docs.goclaw.sh/#teams-what-are-teams).

## Herramientas Integradas

| Herramienta        | Grupo         | Descripción                                                  |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Leer contenido de archivos (con enrutamiento de FS virtual)  |
| `write_file`       | fs            | Escribir/crear archivos                                      |
| `edit_file`        | fs            | Aplicar ediciones específicas a archivos existentes          |
| `list_files`       | fs            | Listar contenido de directorios                              |
| `search`           | fs            | Buscar contenido de archivos por patrón                      |
| `glob`             | fs            | Encontrar archivos por patrón glob                           |
| `exec`             | runtime       | Ejecutar comandos de shell (con flujo de aprobación)         |
| `web_search`       | web           | Buscar en la web (Brave, DuckDuckGo)                         |
| `web_fetch`        | web           | Obtener y analizar contenido web                             |
| `memory_search`    | memory        | Buscar en memoria a largo plazo (FTS + vector)               |
| `memory_get`       | memory        | Recuperar entradas de memoria                                |
| `skill_search`     | —             | Buscar habilidades (híbrido BM25 + embedding)                |
| `knowledge_graph_search` | memory  | Buscar entidades y recorrer relaciones del grafo de conocimiento |
| `create_image`     | media         | Generación de imágenes (DashScope, MiniMax)                  |
| `create_audio`     | media         | Generación de audio (OpenAI, ElevenLabs, MiniMax, Suno)      |
| `create_video`     | media         | Generación de video (MiniMax, Veo)                           |
| `read_document`    | media         | Lectura de documentos (Gemini File API, cadena de proveedores) |
| `read_image`       | media         | Análisis de imágenes                                         |
| `read_audio`       | media         | Transcripción y análisis de audio                            |
| `read_video`       | media         | Análisis de video                                            |
| `message`          | messaging     | Enviar mensajes a canales                                    |
| `tts`              | —             | Síntesis de texto a voz                                      |
| `spawn`            | —             | Lanzar un subagente                                          |
| `subagents`        | sessions      | Controlar subagentes en ejecución                            |
| `team_tasks`       | teams         | Tablero de tareas compartido (listar, crear, reclamar, completar, buscar) |
| `team_message`     | teams         | Buzón del equipo (enviar, transmitir, leer)                  |
| `sessions_list`    | sessions      | Listar sesiones activas                                      |
| `sessions_history` | sessions      | Ver historial de sesiones                                    |
| `sessions_send`    | sessions      | Enviar mensaje a una sesión                                  |
| `sessions_spawn`   | sessions      | Lanzar una nueva sesión                                      |
| `session_status`   | sessions      | Verificar estado de sesión                                   |
| `cron`             | automation    | Programar y gestionar tareas cron                            |
| `gateway`          | automation    | Administración de la pasarela                                |
| `browser`          | ui            | Automatización de navegador (navegar, hacer clic, escribir, captura de pantalla) |
| `announce_queue`   | automation    | Anuncio asíncrono de resultados (para delegaciones asíncronas) |

## Documentación

Documentación completa en **[docs.goclaw.sh](https://docs.goclaw.sh)** — o navega el código fuente en [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Sección | Temas |
|---------|-------|
| [Primeros Pasos](https://docs.goclaw.sh/#what-is-goclaw) | Instalación, Inicio Rápido, Configuración, Tour del Panel Web |
| [Conceptos Fundamentales](https://docs.goclaw.sh/#how-goclaw-works) | Bucle de Agente, Sesiones, Herramientas, Memoria, Multi-Tenancy |
| [Agentes](https://docs.goclaw.sh/#creating-agents) | Crear Agentes, Archivos de Contexto, Personalidad, Compartir y Acceso |
| [Proveedores](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 más |
| [Canales](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Equipos de Agentes](https://docs.goclaw.sh/#teams-what-are-teams) | Equipos, Tablero de Tareas, Mensajería, Delegación y Traspaso |
| [Avanzado](https://docs.goclaw.sh/#custom-tools) | Herramientas Personalizadas, MCP, Habilidades, Cron, Sandbox, Hooks, RBAC |
| [Despliegue](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Base de Datos, Seguridad, Observabilidad, Tailscale |
| [Referencia](https://docs.goclaw.sh/#cli-commands) | Comandos CLI, API REST, Protocolo WebSocket, Variables de Entorno |

## Pruebas

```bash
go test ./...                                    # Pruebas unitarias
go test -v ./tests/integration/ -timeout 120s    # Pruebas de integración (requiere pasarela en ejecución)
```

## Estado del Proyecto

Consulta [CHANGELOG.md](CHANGELOG.md) para el estado detallado de las características, incluyendo qué se ha probado en producción y qué está aún en progreso.

## Agradecimientos

GoClaw está construido sobre el proyecto original [OpenClaw](https://github.com/openclaw/openclaw). Agradecemos la arquitectura y visión que inspiró este port en Go.

## Licencia

MIT
