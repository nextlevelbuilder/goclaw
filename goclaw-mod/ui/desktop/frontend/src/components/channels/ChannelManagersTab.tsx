import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { ManagerGroupCard } from './manager-group-card'
import type { GroupManagerGroupInfo, GroupManagerData, ChannelContact } from '../../types/channel'

interface ChannelManagersTabProps {
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>
  listManagers: (groupId: string) => Promise<GroupManagerData[]>
  addManager: (groupId: string, userId: string, displayName?: string, username?: string) => Promise<void>
  removeManager: (groupId: string, userId: string) => Promise<void>
  listContacts: (search: string) => Promise<ChannelContact[]>
}

export function ChannelManagersTab({
  listManagerGroups, listManagers, addManager, removeManager, listContacts,
}: ChannelManagersTabProps) {
  const { t } = useTranslation('channels')
  const [groups, setGroups] = useState<GroupManagerGroupInfo[]>([])
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [managersMap, setManagersMap] = useState<Record<string, GroupManagerData[]>>({})
  const [loadingMap, setLoadingMap] = useState<Record<string, boolean>>({})
  const [contactOptions, setContactOptions] = useState<{ value: string; label: string }[]>([])
  const [inlineUserId, setInlineUserId] = useState<Record<string, string>>({})
  const [newGroupId, setNewGroupId] = useState('')
  const [newUserId, setNewUserId] = useState('')
  const [addingMap, setAddingMap] = useState<Record<string, boolean>>({})
  const [error, setError] = useState('')

  const loadGroups = useCallback(async () => {
    try { setGroups(await listManagerGroups()) } catch { setGroups([]) }
  }, [listManagerGroups])

  useEffect(() => { loadGroups() }, [loadGroups])

  const handleToggle = async (groupId: string) => {
    const next = !expanded[groupId]
    setExpanded((prev) => ({ ...prev, [groupId]: next }))
    if (next && !managersMap[groupId]) {
      setLoadingMap((prev) => ({ ...prev, [groupId]: true }))
      try {
        setManagersMap((prev) => ({ ...prev, [groupId]: [] }))
        const data = await listManagers(groupId)
        setManagersMap((prev) => ({ ...prev, [groupId]: data }))
      } finally {
        setLoadingMap((prev) => ({ ...prev, [groupId]: false }))
      }
    }
  }

  const handleContactSearch = useCallback(async (search: string) => {
    if (!search || search.length < 2) return
    try {
      const contacts = await listContacts(search)
      setContactOptions(contacts.map((c) => ({
        value: c.sender_id,
        label: c.display_name ? `${c.display_name} (${c.sender_id})` : c.sender_id,
      })))
    } catch { setContactOptions([]) }
  }, [listContacts])

  const handleInlineAdd = async (groupId: string) => {
    const userId = inlineUserId[groupId]?.trim()
    if (!userId) return
    setAddingMap((prev) => ({ ...prev, [groupId]: true }))
    setError('')
    try {
      await addManager(groupId, userId)
      setInlineUserId((prev) => ({ ...prev, [groupId]: '' }))
      setManagersMap((prev) => ({ ...prev, [groupId]: [...(prev[groupId] ?? []), { user_id: userId }] }))
      await loadGroups()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('detail.managers.addForm.errors.failedAdd'))
    } finally {
      setAddingMap((prev) => ({ ...prev, [groupId]: false }))
    }
  }

  const handleRemove = async (groupId: string, userId: string) => {
    setError('')
    try {
      await removeManager(groupId, userId)
      setManagersMap((prev) => ({
        ...prev,
        [groupId]: (prev[groupId] ?? []).filter((m) => m.user_id !== userId),
      }))
      await loadGroups()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove')
    }
  }

  const handleStandaloneAdd = async () => {
    const gid = newGroupId.trim()
    const uid = newUserId.trim()
    if (!gid || !uid) return
    setAddingMap((prev) => ({ ...prev, _new: true }))
    setError('')
    try {
      await addManager(gid, uid)
      setNewGroupId('')
      setNewUserId('')
      await loadGroups()
      setExpanded((prev) => ({ ...prev, [gid]: true }))
      const data = await listManagers(gid)
      setManagersMap((prev) => ({ ...prev, [gid]: data }))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('detail.managers.addForm.errors.failedAdd'))
    } finally {
      setAddingMap((prev) => ({ ...prev, _new: false }))
    }
  }

  const inputClass = 'w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent'

  return (
    <div className="space-y-4">
      <p className="text-xs text-text-muted">{t('detail.managers.description')}</p>
      {error && <p className="text-xs text-error">{error}</p>}

      {groups.length === 0 ? (
        <div className="py-6 text-center">
          <p className="text-xs text-text-muted">{t('detail.managers.noManagerGroups')}</p>
          <p className="text-[11px] text-text-muted/70 mt-1">{t('detail.managers.noManagerGroupsHint')}</p>
        </div>
      ) : (
        <div className="space-y-2">
          {groups.map((g) => (
            <ManagerGroupCard
              key={g.group_id}
              groupId={g.group_id}
              writerCount={g.writer_count}
              expanded={!!expanded[g.group_id]}
              loading={!!loadingMap[g.group_id]}
              managers={managersMap[g.group_id] ?? []}
              adding={!!addingMap[g.group_id]}
              inlineUserId={inlineUserId[g.group_id] ?? ''}
              contactOptions={contactOptions}
              onToggle={() => handleToggle(g.group_id)}
              onInlineUserIdChange={(v) => setInlineUserId((prev) => ({ ...prev, [g.group_id]: v }))}
              onContactSearch={handleContactSearch}
              onInlineAdd={() => handleInlineAdd(g.group_id)}
              onRemove={(userId) => handleRemove(g.group_id, userId)}
            />
          ))}
        </div>
      )}

      {/* Standalone add form for new groups */}
      <div className="border border-border rounded-lg p-4 space-y-3">
        <p className="text-xs font-medium text-text-secondary">{t('detail.managers.addForm.title')}</p>
        <p className="text-[11px] text-text-muted">{t('detail.managers.addForm.hint')}</p>
        <div className="space-y-2">
          <input
            value={newGroupId}
            onChange={(e) => setNewGroupId(e.target.value)}
            placeholder={t('detail.managers.addForm.groupIdPlaceholder')}
            className={inputClass}
          />
          <div className="flex gap-2">
            <div className="flex-1">
              <Combobox
                value={newUserId}
                onChange={(v) => { setNewUserId(v); handleContactSearch(v) }}
                options={contactOptions}
                placeholder={t('detail.managers.addForm.userIdPlaceholder')}
              />
            </div>
            <button
              onClick={handleStandaloneAdd}
              disabled={addingMap['_new'] || !newGroupId.trim() || !newUserId.trim()}
              className="px-3 py-1.5 bg-accent text-white text-xs rounded-lg disabled:opacity-50 cursor-pointer hover:bg-accent-hover transition-colors shrink-0"
            >
              {t('detail.managers.addForm.addManager')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
