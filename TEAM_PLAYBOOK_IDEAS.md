# Team Playbook & Coordination — Future Phase

## Problem

Lead agent thiếu "bản đồ" để lead team hiệu quả:
- Không biết member nào giỏi gì
- Không có strategy chia task
- Không có decision framework: delegate vs tự làm

## Approach: 2 layers

### Layer 1: Team Playbook (static strategy)

File `TEAM_PLAYBOOK.md` per team — human-authored, load vào lead agent context.

```markdown
# Team: Content Pipeline

## Members
- **researcher** (member): web search, data gathering, fact-checking
- **writer** (member): content creation, editing, formatting
- **reviewer** (reviewer): quality check, feedback via comments

## Delegation Strategy
- Research tasks → researcher
- Writing/editing → writer
- Final review → reviewer (workspace comments, not direct edit)
- Complex tasks needing both → researcher first, handoff to writer

## Rules
- Never delegate more than 3 tasks simultaneously
- Always include context/requirements in task description
- Research tasks: max 2 web searches before summarizing
```

**Implementation:**
- Store in `agent_teams.settings.playbook` (text field) hoặc workspace file tagged `reference`
- Inject vào lead agent system prompt khi delegation
- Tool: `team_tasks action=set_playbook content="..."` (lead only)
- Hoặc đơn giản hơn: `workspace_write file_name="TEAM_PLAYBOOK.md"` + auto-inject

### Layer 2: Member Capabilities (structured metadata)

```sql
ALTER TABLE agent_team_members ADD COLUMN capabilities JSONB DEFAULT '{}';
```

```json
{
  "skills": ["coding", "research", "writing"],
  "languages": ["go", "python"],
  "tools": ["web_search", "browser", "exec"],
  "max_concurrent_tasks": 2,
  "notes": "Good at data analysis, slow at creative writing"
}
```

**Tool:** `team_tasks action=capabilities agent_id=... capabilities={...}` (lead only)
**Query:** Lead agent gọi `team_tasks action=list_members` → thấy capabilities → match với task requirements

### Layer 3: Auto-learned patterns (future)

- Track delegation success rate per member per task type
- Lead agent learns: "researcher completes research tasks 90% quality, writer only 60%"
- Suggest optimal assignment based on history
- Needs: task completion quality metric, delegation history analysis

## Priority

- **Now:** Không block workspace feature. Ghi note để plan sau.
- **Next phase:** Layer 1 (playbook) — ít effort, dùng workspace file + system prompt injection
- **Later:** Layer 2 (capabilities) — cần migration + tool changes
- **Future:** Layer 3 (auto-learn) — complex, cần metrics infrastructure

## UI Integration Points

### 1. Add Member flow — prompt playbook input

Khi admin add member vào team (UI), hiện thêm section:
- **Role description**: textarea mô tả member này làm gì trong team
- **Capabilities tags**: multi-select hoặc free-text input (coding, research, writing, review...)
- **Notes**: free-text cho lead agent reference

→ Data lưu vào `agent_team_members.capabilities` JSONB
→ Auto-append vào `TEAM_PLAYBOOK.md` section "## Members"

### 2. Team Settings — Playbook editor

Trong team detail page, thêm tab hoặc section:
- **Playbook editor**: markdown editor cho `TEAM_PLAYBOOK.md`
- **Pre-filled template** khi team chưa có playbook:
  ```markdown
  # Team Playbook

  ## Members
  (auto-generated from member capabilities)

  ## Delegation Strategy
  - Describe how to assign tasks to members...

  ## Rules
  - Max concurrent tasks per member: ...
  - Escalation policy: ...
  ```
- **Preview mode**: render markdown để review trước khi save
- **Auto-sync**: khi add/remove member → suggest update playbook

### 3. Team Review / Audit view

Khi human review team performance:
- **Member workload**: tasks per member, completion rate
- **Playbook compliance**: delegation có follow playbook rules không
- **Suggest improvements**: based on delegation history patterns
- **Playbook diff**: so sánh playbook versions (nếu dùng workspace versioning)

### 4. Member Detail trong team

Click vào member → xem:
- Capabilities (structured)
- Current assigned tasks
- Completion history
- Lead's notes về member
- **Edit capabilities** button (lead only)

## Dependencies

- Workspace feature (đang implement) — playbook có thể là workspace file
- Task system gaps (đang plan) — task_type giúp track delegation patterns
- Escalation policy — playbook define escalation rules per team
