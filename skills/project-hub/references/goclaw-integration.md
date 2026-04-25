# Goclaw Integration Spec — project-hub

> Technical specification for integrating project-hub skill with Goclaw AI Agent Gateway.

## Overview

```
Goclaw Agent Gateway
├── Channels (Telegram/Lark/Zalo/Discord/WhatsApp)
│   └── Project group messages → Extract raw source
├── Memory System (pgvector 3-tier)
│   ├── Working Memory → Conversation context
│   ├── Episodic Memory → Session summaries, interview transcripts
│   └── Semantic Memory → Facts, entities, KG
├── Knowledge Vault
│   └── Documents with [[wikilinks]] → Project knowledge
├── Tasks
│   └── Task ticker + delegation → Sync with AI Hub tasks
└── AI Hub MCP
    └── CRUD operations → AI Hub Knowledge pages
```

---

## 1. Memory System Integration

### 1.1 Working Memory
- **What:** Current conversation context
- **Use case:** Real-time project updates during chat
- **Extract:** Immediate task updates, quick decisions
- **Retention:** Session-scoped

### 1.2 Episodic Memory
- **What:** Session summaries, consolidated from working memory
- **Use case:** Daily digests, interview transcripts
- **Extract:** Tasks, risks, decisions with full context
- **Retention:** Long-term, immutable after consolidation

**Schema:**
```go
type EpisodicMemory struct {
    ID          uuid.UUID
    SessionID   uuid.UUID
    AgentID     uuid.UUID
    UserID      uuid.UUID
    Summary     string
    Topics      []string
    Entities    []Entity
    CreatedAt   time.Time
}
```

**Source reference format:**
```markdown
- **Source:** goclaw-episodic:{session_id}
- **Date:** {created_at}
- **Agent:** {agent_key}
```

### 1.3 Semantic Memory
- **What:** Extracted facts, entities, relationships (KG)
- **Use case:** Long-term project knowledge
- **Extract:** Confirmed facts, entity relationships
- **Retention:** Permanent, can update with new evidence

**Schema:**
```go
type SemanticFact struct {
    ID          uuid.UUID
    Subject     string
    Predicate   string
    Object      string
    Confidence  float64
    Sources     []uuid.UUID  // episodic IDs
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

**Source reference format:**
```markdown
- **Source:** goclaw-semantic:{fact_id}
- **Subject:** {subject}
- **Confidence:** {confidence}
```

---

## 2. Knowledge Vault Integration

### 2.1 Document Registry
- **What:** Project documents with metadata
- **Use case:** Specs, reports, meeting notes
- **Link:** [[wikilinks]] for cross-reference

**Source reference format:**
```markdown
- **Source:** goclaw-vault:{doc_id}
- **Title:** {doc_title}
- **Path:** {doc_path}
```

### 2.2 Hybrid Search
- Use for project query: "tìm meeting nói về X"
- Combines: keyword + semantic + KG traversal

---

## 3. Channel Integration

### 3.1 Supported Platforms

| Platform | Package | Message Format |
|----------|---------|----------------|
| Telegram | `internal/channels/telegram` | Markdown → HTML via `markdownToTelegramHTML()` |
| Lark/Feishu | `internal/channels/feishu` | Markdown → Lark card |
| Zalo | `internal/channels/zalo` | Plain text |
| Discord | `internal/channels/discord` | Discord markdown |
| WhatsApp | `internal/channels/whatsapp` | WhatsApp markdown |

### 3.2 Project Channel Mapping

State file config:
```markdown
## Channel Config
- Platform: telegram
- Chat ID: -1001234567890
- Thread ID: 123 (optional, for topics)
- Owner mapping:
  - NTA: @nta_username
  - Hùng: @hung_pm
- Alert enabled: true
- Alert batch interval: 1h
```

### 3.3 Message Extraction

**From Telegram:**
```go
type TelegramMessage struct {
    MessageID   int64
    ChatID      int64
    FromUser    string
    Text        string
    Date        time.Time
    ReplyTo     *int64
    Attachments []Attachment
}
```

**Source reference format:**
```markdown
- **Source:** goclaw-channel:telegram:{chat_id}:{message_id}
- **From:** {from_user}
- **Date:** {date}
```

### 3.4 Posting Alerts

```go
// Alert posting via Goclaw Channels
func PostProjectAlert(ctx context.Context, projectSlug string, alert Alert) error {
    config := GetChannelConfig(projectSlug)
    
    message := FormatAlert(alert, config.Platform)
    
    switch config.Platform {
    case "telegram":
        return telegram.SendMessage(config.ChatID, message, config.ThreadID)
    case "feishu":
        return feishu.SendCard(config.ChatID, message)
    // ...
    }
}
```

---

## 4. Task System Integration

### 4.1 Goclaw Tasks → AI Hub Tasks

**Sync direction:** Goclaw → AI Hub (primary)

```go
type GoclawTask struct {
    ID          uuid.UUID
    Title       string
    Description string
    Owner       string
    Status      string  // pending, in_progress, done, blocked
    Deadline    *time.Time
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Source      string  // goclaw-episodic:{id}
}
```

**Mapping to AI Hub task format:**
```markdown
| ID | Title | Owner | Status | Start | Deadline | Deps | Source | Notes |
|----|-------|-------|--------|-------|----------|------|--------|-------|
| T1-01 | {title} | {owner} | {status_emoji} | {created_at} | {deadline} | - | goclaw-task:{id} | {description} |
```

### 4.2 Task Ticker Integration

Goclaw Task Ticker runs periodic checks:
- Recovery: stale tasks
- Follow-up: pending items

**Hook into project-hub:**
```go
// On task status change
func OnTaskStatusChange(ctx context.Context, task GoclawTask) {
    // Trigger Workflow F2 for affected project
    projectSlug := GetProjectFromTask(task)
    TriggerAutoIngest(projectSlug, "task-change", task.ID)
}
```

---

## 5. AI Hub MCP Operations

### 5.1 Available Tools

| Tool | Use Case |
|------|----------|
| `create_knowledge` | New project pages, raw sources |
| `update_knowledge` | Sync updates, add items |
| `get_knowledge` | Read existing pages |
| `search_knowledges` | Query projects, audit trail |
| `create_category` | New project category |

### 5.2 Sync Logic

```python
def sync_to_ai_hub(project_slug: str, changes: list[Change]):
    state = read_state(project_slug)
    
    for change in changes:
        page_type = get_affected_page(change)
        page_id = state.pages[page_type].knowledge_id
        
        # Read current content
        current = get_knowledge(page_id)
        
        # Merge changes
        updated = merge_changes(current, change)
        
        # Update AI Hub
        update_knowledge(
            knowledge_id=page_id,
            markdown_content=updated,
            change_summary=f"Auto-sync: {change.summary}"
        )
    
    # Update state
    state.last_sync = now()
    state.hash = compute_hash(changes)
    save_state(state)
```

### 5.3 Conflict Resolution

| Scenario | Resolution |
|----------|------------|
| Goclaw newer | Goclaw wins, overwrite AI Hub |
| AI Hub newer | Merge, prefer AI Hub for manual edits |
| Both changed | Merge, flag conflicts for review |

---

## 6. Event Hooks

### 6.1 Consolidation Events

```go
// Hook: Episodic memory created
bus.Subscribe("consolidation.EpisodicCreated", func(event EpisodicCreatedEvent) {
    // Check if relevant to any project
    projects := FindProjectsByEntities(event.Entities)
    
    for _, project := range projects {
        // Trigger auto-ingest
        TriggerWorkflowF2(project, event.EpisodicID)
    }
})
```

### 6.2 Task Events

```go
// Hook: Task status changed
bus.Subscribe("task.StatusChanged", func(event TaskStatusChangedEvent) {
    project := GetProjectFromTask(event.TaskID)
    
    // Update AI Hub tasks page
    TriggerWorkflowC(project, "tasks", event)
    
    // Check alert triggers
    if event.NewStatus == "blocked" || IsOverdue(event) {
        TriggerWorkflowG(project, event)
    }
})
```

### 6.3 Memory Events

```go
// Hook: Semantic fact extracted
bus.Subscribe("memory.FactExtracted", func(event FactExtractedEvent) {
    // Check if fact is project-relevant
    if IsProjectFact(event.Fact) {
        project := GetProjectFromFact(event.Fact)
        
        // Add to decisions/risks if applicable
        ClassifyAndIngest(project, event.Fact)
    }
})
```

---

## 7. Configuration

### 7.1 Agent Context

```go
type ProjectHubContext struct {
    AgentKey      string
    ProjectSlug   string
    ChannelConfig ChannelConfig
    SyncInterval  time.Duration
    AlertRules    []AlertRule
    AutoApply     bool  // true for high-confidence
}
```

### 7.2 State File Extension

```markdown
## Goclaw Integration
- Agent Key: {agent_key}
- Last Sync: DD/MM/YYYY HH:MM
- Sync Hash: {hash}

## Channel Config
- Platform: telegram
- Chat ID: {chat_id}
- Owner Mapping: {...}
- Alert Enabled: true

## Event Subscriptions
- consolidation.EpisodicCreated: enabled
- task.StatusChanged: enabled
- memory.FactExtracted: enabled

## Sync Queue
| Pending | Type | Source | Confidence | Queued At |
```

---

## 8. Error Handling

| Error | Recovery |
|-------|----------|
| AI Hub unreachable | Queue changes, retry with backoff |
| Channel post fail | Retry 3x, fallback to AI Hub log |
| Memory read fail | Use cached state, warn stale |
| Sync conflict | Flag for manual review |

---

## 9. Security

- **Tenant isolation:** All operations scoped to tenant_id
- **RBAC:** Respect Goclaw permission levels
- **Audit:** Log all AI Hub writes with agent_key
- **Secrets:** API keys in Goclaw vault, not state files

---

## 10. Testing

### Integration Tests

```go
func TestWorkflowF2_AutoIngest(t *testing.T) {
    // Setup: Create episodic memory with task mention
    episodic := CreateTestEpisodic("task X hoàn thành 80%")
    
    // Trigger: Run Workflow F2
    result := TriggerWorkflowF2(testProject, episodic.ID)
    
    // Assert: Task extracted and applied
    assert.Equal(t, 1, len(result.Applied))
    assert.Equal(t, "high", result.Applied[0].Confidence)
    
    // Assert: AI Hub updated
    tasks := GetAIHubPage(testProject, "tasks")
    assert.Contains(t, tasks, "80%")
}

func TestWorkflowG_AlertGeneration(t *testing.T) {
    // Setup: Create overdue task
    task := CreateTestTask(deadline: yesterday)
    
    // Trigger: Run Workflow G
    alerts := TriggerWorkflowG(testProject)
    
    // Assert: Alert generated
    assert.Equal(t, 1, len(alerts))
    assert.Equal(t, "task_overdue", alerts[0].Type)
    
    // Assert: Posted to channel
    messages := GetChannelMessages(testProject)
    assert.Contains(t, messages[0], "[Alert] Task Overdue")
}
```
