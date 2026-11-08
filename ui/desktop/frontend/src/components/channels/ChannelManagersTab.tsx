import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, EmptyState, ErrorState, Field, Input, SkeletonList } from '../ui'
import { Combobox } from '../common/Combobox'
import { ManagerGroupCard } from './manager-group-card'
import type { GroupManagerGroupInfo, GroupManagerData, ChannelContact } from '../../types/channel'

interface ChannelManagersTabProps {
  /** Disables add and remove while the gateway is unreachable. */
  readOnly?: boolean
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>
  listManagers: (groupId: string) => Promise<GroupManagerData[]>
  addManager: (groupId: string, userId: string, displayName?: string, username?: string) => Promise<void>
  removeManager: (groupId: string, userId: string) => Promise<void>
  listContacts: (search: string) => Promise<ChannelContact[]>
}

export function ChannelManagersTab({
  readOnly = false, listManagerGroups, listManagers, addManager, removeManager, listContacts,
}: ChannelManagersTabProps) {
  const { t } = useTranslation('channels')
  const [groups, setGroups] = useState<GroupManagerGroupInfo[]>([])
  const [groupsLoading, setGroupsLoading] = useState(true)
  const [groupsFailed, setGroupsFailed] = useState(false)
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
    setGroupsFailed(false)
    try {
      setGroups(await listManagerGroups())
    } catch {
      setGroups([])
      setGroupsFailed(true)
    } finally {
      setGroupsLoading(false)
    }
  }, [listManagerGroups])

  useEffect(() => { void loadGroups() }, [loadGroups])

  const handleToggle = async (groupId: string) => {
    const next = !expanded[groupId]
    setExpanded((prev) => ({ ...prev, [groupId]: next }))
    if (next && !managersMap[groupId]) {
      setLoadingMap((prev) => ({ ...prev, [groupId]: true }))
      try {
        setManagersMap((prev) => ({ ...prev, [groupId]: [] }))
        const data = await listManagers(groupId)
        setManagersMap((prev) => ({ ...prev, [groupId]: data }))
      } catch (err) {
        setError(err instanceof Error ? err.message : t('detail.managers.loadError'))
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
      setError(err instanceof Error ? err.message : t('detail.managers.removeFailed'))
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

  let groupsBody
  if (groupsLoading) {
    groupsBody = <SkeletonList rows={2} avatar={false} label={t('detail.managers.loadingManagers')} />
  } else if (groupsFailed) {
    groupsBody = <ErrorState title={t('detail.managers.loadError')} description={t('screen.loadErrorHint')} onRetry={() => { void loadGroups() }} className="py-8" />
  } else if (groups.length === 0) {
    groupsBody = <EmptyState title={t('detail.managers.noManagerGroups')} description={t('detail.managers.noManagerGroupsHint')} className="py-8" />
  } else {
    groupsBody = (
      <div className="flex flex-col gap-2">
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
            readOnly={readOnly}
            onToggle={() => { void handleToggle(g.group_id) }}
            onInlineUserIdChange={(v) => setInlineUserId((prev) => ({ ...prev, [g.group_id]: v }))}
            onContactSearch={handleContactSearch}
            onInlineAdd={() => { void handleInlineAdd(g.group_id) }}
            onRemove={(userId) => { void handleRemove(g.group_id, userId) }}
          />
        ))}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-caption text-ink-muted">{t('detail.managers.description')}</p>
      {error && <p role="alert" className="text-caption text-danger">{error}</p>}

      {groupsBody}

      {/* Standalone add form for new groups */}
      <fieldset disabled={readOnly} className="m-0 flex min-w-0 flex-col gap-3 rounded-lg border border-border-subtle bg-surface-base p-4">
        <legend className="sr-only">{t('detail.managers.addForm.title')}</legend>
        <div>
          <h3 className="text-ui font-medium text-ink">{t('detail.managers.addForm.title')}</h3>
          <p className="text-caption text-ink-muted">{t('detail.managers.addForm.hint')}</p>
        </div>
        <Field label={t('detail.managers.addForm.groupId')}>
          <Input
            value={newGroupId}
            onChange={(e) => setNewGroupId(e.target.value)}
            placeholder={t('detail.managers.addForm.groupIdPlaceholder')}
          />
        </Field>
        <div className="flex flex-col gap-1.5">
          <span className="text-ui text-ink">{t('detail.managers.addForm.userId')}</span>
          <div className="flex items-center gap-2">
            <div className="min-w-0 flex-1">
              <Combobox
                value={newUserId}
                onChange={(v) => { setNewUserId(v); void handleContactSearch(v) }}
                options={contactOptions}
                placeholder={t('detail.managers.addForm.userIdPlaceholder')}
                disabled={readOnly}
              />
            </div>
            <Button
              onClick={() => { void handleStandaloneAdd() }}
              loading={!!addingMap['_new']}
              disabled={readOnly || !newGroupId.trim() || !newUserId.trim()}
            >
              {t('detail.managers.addForm.addManager')}
            </Button>
          </div>
        </div>
      </fieldset>
    </div>
  )
}
