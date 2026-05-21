import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Database,
  GraduationCap,
  HardDrive,
  Layers,
  ListChecks,
  MonitorUp,
  Network,
  Play,
  Plus,
  RefreshCw,
  Save,
  Server,
  Settings,
  ShieldCheck,
  Snowflake,
  TerminalSquare,
  Trash2,
  User,
} from 'lucide-react'
import './App.css'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const statusFlow = [
  'REQUESTED',
  'ALLOCATING_PROJECT',
  'CHECKING_CAPACITY',
  'DEPLOYING',
  'ISSUING_VDI_ACCESS',
  'READY',
  'VERIFYING',
  'VERIFIED',
  'VERIFICATION_FAILED',
  'FROZEN',
  'CLEANING',
  'FINISHED',
  'FAILED',
]

const defaultSettings = {
  lab_ttl_seconds: 7200,
  freeze_ttl_seconds: 86400,
  capacity_threshold_percent: 90,
}

const emptyLabDefinition = {
  course_id: 'course-3',
  lab_id: '',
  title: '',
  description: '',
  enabled: true,
  resources: {
    vcpu: 1,
    ram_mib: 1024,
    disk_gib: 20,
  },
  instances: [
    {
      name: '',
      image_id: '',
      flavor_id: '',
      fixed_ip: '',
      disk_gib: 20,
    },
  ],
}

function App() {
  const [mode, setMode] = useState('student')
  const [labs, setLabs] = useState([])
  const [availableLabs, setAvailableLabs] = useState([])
  const [teacherLabs, setTeacherLabs] = useState([])
  const [selectedLabKey, setSelectedLabKey] = useState('')
  const [teacherLabKey, setTeacherLabKey] = useState('')
  const [draftLab, setDraftLab] = useState(emptyLabDefinition)
  const [selectedID, setSelectedID] = useState('')
  const [selectedLab, setSelectedLab] = useState(null)
  const [selectedVDI, setSelectedVDI] = useState(null)
  const [labInstances, setLabInstances] = useState([])
  const [projectPool, setProjectPool] = useState({ states: {}, projects: [] })
  const [auditRows, setAuditRows] = useState([])
  const [checkRuns, setCheckRuns] = useState([])
  const [settings, setSettings] = useState(defaultSettings)
  const [studentID, setStudentID] = useState('')
  const [notice, setNotice] = useState('Ожидание данных от API')
  const [loading, setLoading] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  const selectedDefinition = useMemo(() => {
    return availableLabs.find((lab) => labKey(lab) === selectedLabKey) ?? availableLabs[0] ?? null
  }, [availableLabs, selectedLabKey])
  const normalizedStudentID = studentID.trim()
  const selectedRun = selectedLab ?? labs.find((lab) => lab.id === selectedID) ?? null
  const teacherActiveLab = selectedRun ?? labs[0] ?? null
  const studentRun = useMemo(() => {
    return findRunForDefinition(labs, selectedDefinition, normalizedStudentID)
  }, [labs, normalizedStudentID, selectedDefinition])
  const activeLab = mode === 'student'
    ? (labMatchesDefinition(selectedRun, selectedDefinition, normalizedStudentID) ? selectedRun : studentRun)
    : teacherActiveLab
  const activeLabID = activeLab?.id ?? ''
  const capacity = useMemo(() => capacityFromEvents(activeLab?.events), [activeLab])
  const cleanupDueAt = cleanupDueDate(activeLab)
  const remaining = cleanupDueAt ? cleanupDueAt.getTime() - now : null
  const activeCount = labs.filter((lab) => !['FAILED', 'FINISHED'].includes(lab.state)).length
  const freeProjects = projectPool.states?.FREE ?? 0
  const latestCheck = checkRuns[0]
  const canOperateLab = activeLab && !['CLEANING', 'FAILED', 'FINISHED'].includes(activeLab.state)
  const activeInstances = labInstances.filter((instance) => instance.state === 'ACTIVE').length
  const availableVDI = labInstances.filter((instance) => instance.vdi_access?.available).length
  const configuredVCPU = selectedDefinition?.resources?.vcpu ?? 0
  const configuredRAM = Math.round((selectedDefinition?.resources?.ram_mib ?? 0) / 1024)
  const configuredDisk = selectedDefinition?.resources?.disk_gib ?? 0
  const instanceCount = labInstances.length || selectedDefinition?.instances?.length || 0

  const requestJSON = useCallback(async (path, options = {}) => {
    const response = await fetch(`${API_BASE}${path}`, {
      headers: { 'Content-Type': 'application/json', ...(options.headers ?? {}) },
      ...options,
    })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) {
      throw new Error(payload.error?.message ?? response.statusText)
    }
    return payload
  }, [])

  const loadLabDefinitions = useCallback(async () => {
    const [studentCatalog, teacherCatalog] = await Promise.all([
      requestJSON('/api/lab-definitions').catch(() => ({ labs: [] })),
      requestJSON('/api/teacher/lab-definitions').catch(() => ({ labs: [] })),
    ])
    const nextAvailable = studentCatalog.labs ?? []
    const nextTeacherLabs = teacherCatalog.labs ?? []
    setAvailableLabs(nextAvailable)
    setTeacherLabs(nextTeacherLabs)
    setSelectedLabKey((current) => current || labKey(nextAvailable[0]))
    setTeacherLabKey((current) => current || labKey(nextTeacherLabs[0]))
    setDraftLab((current) => {
      if (current?.lab_id) {
        return current
      }
      return normalizeDefinition(nextTeacherLabs[0] ?? emptyLabDefinition)
    })
    return { available: nextAvailable, teacher: nextTeacherLabs }
  }, [requestJSON])

  const loadLabs = useCallback(async () => {
    const payload = await requestJSON('/api/labs?limit=30')
    const nextLabs = payload.labs ?? []
    setLabs(nextLabs)
    setSelectedID((current) => current || nextLabs[0]?.id || '')
    return nextLabs
  }, [requestJSON])

  const loadProjectPool = useCallback(async () => {
    const payload = await requestJSON('/api/admin/project-pool')
    setProjectPool({ states: payload.states ?? {}, projects: payload.projects ?? [] })
  }, [requestJSON])

  const loadSettings = useCallback(async () => {
    const payload = await requestJSON('/api/admin/settings')
    setSettings({ ...defaultSettings, ...(payload.values ?? {}) })
  }, [requestJSON])

  const loadAudit = useCallback(async () => {
    const payload = await requestJSON('/api/admin/audit?limit=12')
    setAuditRows(payload.events ?? [])
  }, [requestJSON])

  const loadSelectedDetails = useCallback(async (labRunID) => {
    if (!labRunID) {
      setSelectedLab(null)
      setSelectedVDI(null)
      setLabInstances([])
      setCheckRuns([])
      return
    }
    const [lab, checks, vdi, instances] = await Promise.all([
      requestJSON(`/api/labs/${labRunID}`),
      requestJSON(`/api/labs/${labRunID}/checks?limit=3`).catch(() => ({ runs: [] })),
      requestJSON(`/api/labs/${labRunID}/vdi`).catch(() => null),
      requestJSON(`/api/labs/${labRunID}/instances`).catch(() => ({ instances: [] })),
    ])
    setSelectedLab(lab)
    setSelectedVDI(vdi)
    setLabInstances(instances.instances ?? [])
    setCheckRuns(checks.runs ?? [])
  }, [requestJSON])

  const waitForLabInReadModel = useCallback(async (labRunID) => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      const nextLabs = await loadLabs()
      if (nextLabs.some((lab) => lab.id === labRunID)) {
        return true
      }
      await sleep(500)
    }
    return false
  }, [loadLabs])

  const refreshAll = useCallback(async () => {
    setLoading(true)
    try {
      const [nextLabs] = await Promise.all([
        loadLabs(),
        loadLabDefinitions(),
        loadProjectPool(),
        loadSettings(),
        loadAudit(),
      ])
      const nextSelected = selectedID || nextLabs[0]?.id || ''
      if (nextSelected) {
        await loadSelectedDetails(nextSelected)
      }
      setNotice('Данные обновлены из backend read model')
    } catch (error) {
      setNotice(`Ошибка обновления: ${error.message}`)
    } finally {
      setLoading(false)
    }
  }, [loadAudit, loadLabDefinitions, loadLabs, loadProjectPool, loadSelectedDetails, loadSettings, selectedID])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      refreshAll()
    }, 0)
    return () => window.clearTimeout(timer)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadSelectedDetails(activeLabID).catch((error) => setNotice(`Ошибка стенда: ${error.message}`))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [activeLabID, loadSelectedDetails])

  useEffect(() => {
    if (!activeLabID || typeof EventSource === 'undefined') {
      return undefined
    }
    const source = new EventSource(`${API_BASE}/api/labs/${activeLabID}/events`)
    source.addEventListener('lab_run', (event) => {
      const payload = JSON.parse(event.data)
      setNotice(`Live update: ${payload.state}`)
      loadSelectedDetails(activeLabID).catch((error) => setNotice(`Ошибка live update: ${error.message}`))
      loadLabs().catch(() => {})
    })
    source.onerror = () => {
      source.close()
    }
    return () => source.close()
  }, [activeLabID, loadLabs, loadSelectedDetails])

  async function sendCommand(path, body = {}) {
    setNotice(`Команда отправляется: ${path}`)
    try {
      const payload = await requestJSON(path, {
        method: 'POST',
        body: JSON.stringify(body),
      })
      setNotice(`Команда принята: ${payload.command_id ?? payload.lab_run_id ?? 'ok'}`)
      if (payload.lab_run_id) {
        const visible = await waitForLabInReadModel(payload.lab_run_id)
        if (visible) {
          setSelectedID(payload.lab_run_id)
          await Promise.all([loadSelectedDetails(payload.lab_run_id), loadProjectPool(), loadSettings(), loadAudit()])
          return payload
        }
      }
      await refreshAll()
      return payload
    } catch (error) {
      setNotice(`Ошибка команды: ${error.message}`)
      return null
    }
  }

  function requestSelectedLab() {
    if (!normalizedStudentID) {
      setNotice('Укажи student_id перед запуском лабораторной')
      return
    }
    if (!selectedDefinition) {
      setNotice('Нет доступной лабораторной работы')
      return
    }
    sendCommand('/api/labs', {
      student_id: normalizedStudentID,
      course_id: selectedDefinition.course_id,
      lab_id: selectedDefinition.lab_id,
      source: 'student-ui',
      idempotency_key: `lab:${selectedDefinition.course_id}:${selectedDefinition.lab_id}:${normalizedStudentID}:${Date.now()}`,
    })
  }

  function freezeLab() {
    if (!activeLab) return
    sendCommand(`/api/labs/${activeLab.id}/freeze`, {
      reason: 'support_freeze',
      idempotency_key: `freeze:${activeLab.id}:${Date.now()}`,
    })
  }

  function cleanupLab(reason = 'student_cleanup') {
    if (!activeLab) return
    sendCommand(`/api/labs/${activeLab.id}/cleanup`, {
      reason,
      idempotency_key: `cleanup:${activeLab.id}:${Date.now()}`,
    })
  }

  function checkLab() {
    if (!activeLab) return
    sendCommand(`/api/labs/${activeLab.id}/check`, {
      profile_id: 'default',
      idempotency_key: `check:${activeLab.id}:${Date.now()}`,
    })
  }

  function openInstanceVDI(instance) {
    const url = instance?.vdi_access?.url
    if (!url) return
    window.open(url, '_blank', 'noopener,noreferrer')
  }

  function saveSettings() {
    sendCommand('/api/admin/settings', {
      changed_by: 'teacher-console',
      values: settings,
      idempotency_key: `settings:${Date.now()}`,
    })
  }

  async function saveLabDefinition() {
    const payload = normalizeDefinition(draftLab)
    setNotice(`Сохранение конфигурации: ${payload.course_id}/${payload.lab_id}`)
    try {
      const result = await requestJSON('/api/teacher/lab-definitions', {
        method: 'POST',
        body: JSON.stringify({ ...payload, changed_by: 'teacher-console' }),
      })
      setNotice(`Конфигурация сохранена: ${result.lab.course_id}/${result.lab.lab_id}`)
      await loadLabDefinitions()
      setTeacherLabKey(labKey(result.lab))
      setDraftLab(normalizeDefinition(result.lab))
      setSelectedLabKey((current) => current || labKey(result.lab))
    } catch (error) {
      setNotice(`Ошибка конфигурации: ${error.message}`)
    }
  }

  function selectTeacherLab(nextKey) {
    setTeacherLabKey(nextKey)
    const next = teacherLabs.find((lab) => labKey(lab) === nextKey)
    if (next) {
      setDraftLab(normalizeDefinition(next))
    }
  }

  function createNewLabDefinition() {
    setTeacherLabKey('')
    setDraftLab(normalizeDefinition(emptyLabDefinition))
  }

  function updateDraft(path, value) {
    setDraftLab((current) => setNestedValue(current, path, value))
  }

  function updateDraftInstance(index, field, value) {
    setDraftLab((current) => {
      const instances = [...(current.instances ?? [])]
      instances[index] = { ...instances[index], [field]: value }
      return { ...current, instances }
    })
  }

  function addDraftInstance() {
    setDraftLab((current) => ({
      ...current,
      instances: [
        ...(current.instances ?? []),
        { name: '', image_id: '', flavor_id: '', fixed_ip: '', disk_gib: 20 },
      ],
    }))
  }

  function removeDraftInstance(index) {
    setDraftLab((current) => ({
      ...current,
      instances: (current.instances ?? []).filter((_, itemIndex) => itemIndex !== index),
    }))
  }

  return (
    <div className={`app-shell ${mode}-space`}>
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark">
            <Server size={21} />
          </div>
          <div>
            <strong>Cyber Deploy Hub</strong>
            <span>Оркестрация лабораторных стендов</span>
          </div>
        </div>
        <div className="role-switch" aria-label="Рабочее пространство">
          <button type="button" className={mode === 'student' ? 'active' : ''} onClick={() => setMode('student')}>
            <User size={16} /> Пространство ученика
          </button>
          <button type="button" className={mode === 'teacher' ? 'active' : ''} onClick={() => setMode('teacher')}>
            <GraduationCap size={16} /> Пространство преподавателя
          </button>
        </div>
      </header>

      <main>
        {mode === 'student' ? (
          <StudentPanel
            activeInstances={activeInstances}
            activeLab={activeLab}
            availableLabs={availableLabs}
            availableVDI={availableVDI}
            canOperateLab={canOperateLab}
            cleanupDueAt={cleanupDueAt}
            configuredDisk={configuredDisk}
            configuredRAM={configuredRAM}
            configuredVCPU={configuredVCPU}
            freezeLab={freezeLab}
            instanceCount={instanceCount}
            labInstances={labInstances}
            loading={loading}
            notice={notice}
            openInstanceVDI={openInstanceVDI}
            remaining={remaining}
            requestSelectedLab={requestSelectedLab}
            cleanupLab={() => cleanupLab('student_cleanup')}
            refreshAll={refreshAll}
            selectedDefinition={selectedDefinition}
            selectedLabKey={selectedLabKey}
            selectedVDI={selectedVDI}
            setSelectedLabKey={setSelectedLabKey}
            setStudentID={setStudentID}
            studentID={studentID}
          />
        ) : (
          <TeacherPanel
            activeCount={activeCount}
            activeLab={activeLab}
            auditRows={auditRows}
            capacity={capacity}
            checkLab={checkLab}
            cleanupLab={() => cleanupLab('teacher_cleanup')}
            createNewLabDefinition={createNewLabDefinition}
            draftLab={draftLab}
            freeProjects={freeProjects}
            labs={labs}
            latestCheck={latestCheck}
            loading={loading}
            notice={notice}
            projectPool={projectPool}
            refreshAll={refreshAll}
            removeDraftInstance={removeDraftInstance}
            saveLabDefinition={saveLabDefinition}
            saveSettings={saveSettings}
            selectTeacherLab={selectTeacherLab}
            selectedID={selectedID}
            setSelectedID={setSelectedID}
            setSettings={setSettings}
            settings={settings}
            teacherLabKey={teacherLabKey}
            teacherLabs={teacherLabs}
            updateDraft={updateDraft}
            updateDraftInstance={updateDraftInstance}
            addDraftInstance={addDraftInstance}
          />
        )}
      </main>
    </div>
  )
}

function StudentPanel({
  activeInstances,
  activeLab,
  availableLabs,
  availableVDI,
  canOperateLab,
  cleanupDueAt,
  configuredDisk,
  configuredRAM,
  configuredVCPU,
  freezeLab,
  instanceCount,
  labInstances,
  loading,
  notice,
  openInstanceVDI,
  remaining,
  requestSelectedLab,
  cleanupLab,
  refreshAll,
  selectedDefinition,
  selectedLabKey,
  selectedVDI,
  setSelectedLabKey,
  setStudentID,
  studentID,
}) {
  return (
    <>
      <section className="summary-band">
        <div>
          <p className="eyebrow">Student workspace</p>
          <h1>{selectedDefinition?.title ?? 'Доступные лабораторные работы'}</h1>
        </div>
        <div className="summary-actions">
          <label className="field compact">
            <span>student_id</span>
            <input value={studentID} onChange={(event) => setStudentID(event.target.value)} placeholder="moodle:42" />
          </label>
          <label className="field lab-select">
            <span>Лабораторная</span>
            <select value={selectedLabKey} onChange={(event) => setSelectedLabKey(event.target.value)}>
              {availableLabs.map((lab) => (
                <option key={labKey(lab)} value={labKey(lab)}>
                  {lab.title}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="primary" onClick={requestSelectedLab} disabled={loading || !selectedDefinition}>
            <Play size={17} /> Запустить
          </button>
          <IconButton label="Обновить" onClick={refreshAll} disabled={loading} icon={RefreshCw} />
        </div>
      </section>

      <section className="metrics-grid" aria-label="Ключевые метрики">
        <Metric icon={Layers} label="Доступные лабы" value={availableLabs.length} hint={selectedDefinition?.course_id ?? 'каталог пуст'} />
        <Metric icon={Activity} label="Статус стенда" value={activeLab?.state ?? 'нет'} hint={activeLab?.lab_id ?? 'запуск не выбран'} />
        <Metric icon={Clock3} label="До удаления" value={formatRemaining(remaining)} hint={cleanupDueAt ? formatDateTime(cleanupDueAt) : 'таймер не запущен'} />
        <Metric icon={MonitorUp} label="VDI" value={`${availableVDI}/${instanceCount || 0}`} hint={`${activeInstances} VM active`} />
      </section>

      <section className="student-workspace">
        <div className="panel lab-picker">
          <PanelTitle icon={ListChecks} title="Лабораторные работы" right={<span className="muted">{availableLabs.length}</span>} />
          <div className="definition-list">
            {availableLabs.map((lab) => (
              <button
                key={labKey(lab)}
                type="button"
                className={`definition-row ${labKey(lab) === selectedLabKey ? 'active' : ''}`}
                onClick={() => setSelectedLabKey(labKey(lab))}
              >
                <span>
                  <strong>{lab.title}</strong>
                  <small>{lab.course_id} · {lab.lab_id}</small>
                </span>
                <StatusPill state={lab.enabled ? 'READY' : 'DISABLED'} />
              </button>
            ))}
            {availableLabs.length === 0 && <div className="empty-state">Нет опубликованных лабораторных работ.</div>}
          </div>
        </div>

        <LabStatePanel
          activeLab={activeLab}
          canOperateLab={canOperateLab}
          cleanupDueAt={cleanupDueAt}
          cleanupLab={cleanupLab}
          configuredDisk={configuredDisk}
          configuredRAM={configuredRAM}
          configuredVCPU={configuredVCPU}
          freezeLab={freezeLab}
          instanceCount={instanceCount}
          labInstances={labInstances}
          openInstanceVDI={openInstanceVDI}
          remaining={remaining}
          selectedDefinition={selectedDefinition}
          selectedVDI={selectedVDI}
        />
      </section>

      <section className="panel audit-panel">
        <PanelTitle icon={Clock3} title="События" right={<span className="notice">{notice}</span>} />
        <div className="event-strip">
          {(activeLab?.events ?? []).slice(-6).map((event) => (
            <div key={event.id}>
              <span>{formatTime(event.created_at)}</span>
              <code>{event.message_type}</code>
              <StatusPill state={event.state || 'EVENT'} />
            </div>
          ))}
          {!activeLab && <div className="empty-state">Запущенный стенд появится здесь после команды запуска.</div>}
        </div>
      </section>
    </>
  )
}

function LabStatePanel({
  activeLab,
  canOperateLab,
  cleanupDueAt,
  cleanupLab,
  configuredDisk,
  configuredRAM,
  configuredVCPU,
  freezeLab,
  instanceCount,
  labInstances,
  openInstanceVDI,
  remaining,
  selectedDefinition,
  selectedVDI,
}) {
  return (
    <div className="panel detail-panel">
      <PanelTitle icon={Network} title="Стенд" right={<StatusPill state={activeLab?.state ?? 'EMPTY'} />} />
      <div className="flow">
        {statusFlow.map((state) => (
          <div key={state} className={`flow-step ${flowClass(activeLab?.state, state)}`}>
            <span></span>
            <small>{stateLabel(state)}</small>
          </div>
        ))}
      </div>

      <div className="detail-grid">
        <div className="runtime-summary">
          <div>
            <span>Завершение</span>
            <strong>{formatRemaining(remaining)}</strong>
            <small>{cleanupDueAt ? formatDateTime(cleanupDueAt) : 'ожидание события lifecycle'}</small>
          </div>
          <div>
            <span>Лаба</span>
            <strong>{selectedDefinition?.title ?? activeLab?.lab_id ?? 'нет'}</strong>
            <small>{activeLab?.id ?? selectedDefinition?.lab_id ?? 'стенд не запущен'}</small>
          </div>
        </div>
        <div className="resource-table">
          <div><span>vCPU</span><strong>{configuredVCPU}</strong></div>
          <div><span>RAM</span><strong>{configuredRAM} GiB</strong></div>
          <div><span>Disk</span><strong>{configuredDisk} GiB</strong></div>
          <div><span>VM active</span><strong>{labInstances.filter((instance) => instance.state === 'ACTIVE').length}/{instanceCount}</strong></div>
        </div>
      </div>

      <div className="instances-block">
        <div className="instances-head">
          <strong>Виртуальные машины</strong>
          <span>{labInstances.length ? `${labInstances.length} VM` : 'ожидание деплоя'}</span>
        </div>
        <div className="instance-list">
          {labInstances.map((instance) => (
            <div key={`${activeLab?.id}-${instance.name}`} className="instance-row">
              <div className="instance-main">
                <strong>{instance.name}</strong>
                <small>{instance.fixed_ip || 'IP pending'} · {instance.disk_gib} GiB</small>
              </div>
              <StatusPill state={instance.state} />
              <button
                type="button"
                className="secondary"
                disabled={!instance.vdi_access?.available}
                onClick={() => openInstanceVDI(instance)}
              >
                <MonitorUp size={16} /> VDI
              </button>
            </div>
          ))}
          {labInstances.length === 0 && <div className="empty-state">VM появятся после события cloud deployment.</div>}
        </div>
      </div>

      {activeLab?.failure_code && (
        <div className="incident">
          <AlertTriangle size={19} />
          <div>
            <strong>{activeLab.failure_code}</strong>
            <span>{activeLab.state} · {activeLab.id}</span>
            <p>{activeLab.failure_message}</p>
          </div>
        </div>
      )}

      <div className="toolbar">
        <button type="button" className="primary" disabled={!selectedVDI?.available} onClick={() => openInstanceVDI(labInstances.find((instance) => instance.vdi_access?.available))}>
          <MonitorUp size={17} /> VDI
        </button>
        <button type="button" className="secondary" disabled={!canOperateLab} onClick={freezeLab}>
          <Snowflake size={17} /> Freeze
        </button>
        <button type="button" className="danger" disabled={!canOperateLab} onClick={cleanupLab}>
          <Trash2 size={17} /> Cleanup
        </button>
      </div>
    </div>
  )
}

function TeacherPanel({
  activeCount,
  activeLab,
  addDraftInstance,
  auditRows,
  capacity,
  checkLab,
  cleanupLab,
  createNewLabDefinition,
  draftLab,
  freeProjects,
  labs,
  latestCheck,
  loading,
  notice,
  projectPool,
  refreshAll,
  removeDraftInstance,
  saveLabDefinition,
  saveSettings,
  selectTeacherLab,
  selectedID,
  setSelectedID,
  setSettings,
  settings,
  teacherLabKey,
  teacherLabs,
  updateDraft,
  updateDraftInstance,
}) {
  return (
    <>
      <section className="summary-band teacher-band">
        <div>
          <p className="eyebrow">Teacher workspace</p>
          <h1>Конфигурация лабораторных работ</h1>
        </div>
        <div className="summary-actions">
          <label className="field lab-select">
            <span>Конфигурация</span>
            <select value={teacherLabKey} onChange={(event) => selectTeacherLab(event.target.value)}>
              {teacherLabs.map((lab) => (
                <option key={labKey(lab)} value={labKey(lab)}>
                  {lab.title}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="secondary" onClick={createNewLabDefinition}>
            <Plus size={17} /> Новая
          </button>
          <IconButton label="Обновить" onClick={refreshAll} disabled={loading} icon={RefreshCw} />
        </div>
      </section>

      <section className="metrics-grid" aria-label="Ключевые метрики">
        <Metric icon={Activity} label="Активные стенды" value={activeCount} hint={`${teacherLabs.length} конфигураций`} />
        <Metric icon={Database} label="Свободные проекты" value={freeProjects} hint={`${projectPool.projects?.length ?? 0} реальных проектов`} />
        <Metric icon={HardDrive} label="Storage forecast" value={capacity.storage} hint={`threshold ${capacity.threshold}`} />
        <Metric icon={ShieldCheck} label="Проверки SSH" value={latestCheck?.state ?? 'нет'} hint={latestCheck ? `profile ${latestCheck.profile_id}` : 'нет запусков'} />
      </section>

      <section className="teacher-grid">
        <div className="panel config-panel">
          <PanelTitle icon={Settings} title="Конфигурация лабы" right={<StatusPill state={draftLab.enabled ? 'ENABLED' : 'DISABLED'} />} />
          <div className="config-form">
            <TextField label="course_id" value={draftLab.course_id} onChange={(value) => updateDraft(['course_id'], value)} />
            <TextField label="lab_id" value={draftLab.lab_id} onChange={(value) => updateDraft(['lab_id'], value)} />
            <TextField label="Название" value={draftLab.title} onChange={(value) => updateDraft(['title'], value)} />
            <TextField label="Описание" value={draftLab.description} onChange={(value) => updateDraft(['description'], value)} wide />
            <label className="toggle-line">
              <input type="checkbox" checked={draftLab.enabled} onChange={(event) => updateDraft(['enabled'], event.target.checked)} />
              <span>Доступна ученикам</span>
            </label>
          </div>

          <div className="resource-editor">
            <NumberField name="resource-vcpu" label="vCPU" value={draftLab.resources?.vcpu ?? 0} onChange={(value) => updateDraft(['resources', 'vcpu'], value)} />
            <NumberField name="resource-ram" label="RAM, MiB" value={draftLab.resources?.ram_mib ?? 0} onChange={(value) => updateDraft(['resources', 'ram_mib'], value)} />
            <NumberField name="resource-disk" label="Disk, GiB" value={draftLab.resources?.disk_gib ?? 0} onChange={(value) => updateDraft(['resources', 'disk_gib'], value)} />
          </div>

          <div className="instance-editor">
            <div className="instances-head">
              <strong>VM blueprint</strong>
              <button type="button" className="secondary slim" onClick={addDraftInstance}>
                <Plus size={15} /> VM
              </button>
            </div>
            {(draftLab.instances ?? []).map((instance, index) => (
              <div key={`${index}-${instance.name}`} className="instance-edit-row">
                <TextField label="name" value={instance.name} onChange={(value) => updateDraftInstance(index, 'name', value)} />
                <TextField label="image_id" value={instance.image_id} onChange={(value) => updateDraftInstance(index, 'image_id', value)} />
                <TextField label="flavor_id" value={instance.flavor_id} onChange={(value) => updateDraftInstance(index, 'flavor_id', value)} />
                <TextField label="fixed_ip" value={instance.fixed_ip ?? ''} onChange={(value) => updateDraftInstance(index, 'fixed_ip', value)} />
                <NumberField name={`disk-${index}`} label="disk_gib" value={instance.disk_gib ?? 0} onChange={(value) => updateDraftInstance(index, 'disk_gib', value)} />
                <button type="button" className="danger icon-only" onClick={() => removeDraftInstance(index)} aria-label="Удалить VM">
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
          </div>

          <button type="button" className="primary full" onClick={saveLabDefinition}>
            <Save size={17} /> Сохранить конфигурацию
          </button>
        </div>

        <div className="panel labs-panel">
          <PanelTitle icon={MonitorUp} title="Стенды" right={<StatusPill state={activeLab?.state ?? 'EMPTY'} />} />
          <div className="lab-list">
            {labs.map((lab) => (
              <button
                key={lab.id}
                type="button"
                className={`lab-row ${lab.id === selectedID ? 'active' : ''}`}
                onClick={() => setSelectedID(lab.id)}
              >
                <span>
                  <strong>{lab.student_id}</strong>
                  <small>{lab.course_id} · {lab.lab_id}</small>
                </span>
                <StatusPill state={lab.state} />
              </button>
            ))}
            {labs.length === 0 && <div className="empty-state">В read model пока нет лабораторных запусков.</div>}
          </div>
        </div>
      </section>

      <section className="operations-grid">
        <div className="panel">
          <PanelTitle icon={Activity} title="Capacity" right={<span className={capacity.ok ? 'ok' : 'muted'}>{capacity.label}</span>} />
          <div className="capacity-bars">
            <Bar label="CPU" value={capacity.cpuValue} />
            <Bar label="RAM" value={capacity.ramValue} />
            <Bar label="Storage" value={capacity.storageValue} />
          </div>
        </div>

        <div className="panel settings-panel">
          <PanelTitle icon={Settings} title="Lifecycle" />
          <NumberField name="lab_ttl_seconds" label="Lab TTL, sec" value={settings.lab_ttl_seconds} onChange={(value) => setSettings({ ...settings, lab_ttl_seconds: value })} />
          <NumberField name="freeze_ttl_seconds" label="Freeze TTL, sec" value={settings.freeze_ttl_seconds} onChange={(value) => setSettings({ ...settings, freeze_ttl_seconds: value })} />
          <NumberField name="capacity_threshold_percent" label="Capacity threshold, %" value={settings.capacity_threshold_percent} onChange={(value) => setSettings({ ...settings, capacity_threshold_percent: value })} />
          <button type="button" className="primary full" onClick={saveSettings}>
            <CheckCircle2 size={17} /> Применить
          </button>
        </div>

        <div className="panel teacher-actions">
          <PanelTitle icon={TerminalSquare} title="Операции" />
          <button type="button" className="secondary full" disabled={!activeLab} onClick={checkLab}>
            <TerminalSquare size={17} /> Check
          </button>
          <button type="button" className="danger full" disabled={!activeLab} onClick={cleanupLab}>
            <Trash2 size={17} /> Cleanup
          </button>
        </div>

        <div className="panel checks-panel">
          <PanelTitle icon={TerminalSquare} title="SSH-проверка" right={<StatusPill state={latestCheck?.state ?? 'PENDING'} />} />
          <div className="check-results">
            {(latestCheck?.results ?? []).map((result) => (
              <div key={`${latestCheck?.id}-${result.sequence}`} className={result.passed ? 'passed' : 'failed'}>
                <span>{result.sequence}</span>
                <strong>{result.name}</strong>
                <code>{result.type}</code>
                <small>{result.message || `exit ${result.exit_code}`}</small>
              </div>
            ))}
            {!latestCheck && <div className="empty-state">Проверки появятся после команды Check.</div>}
          </div>
        </div>

        <div className="panel" id="pool">
          <PanelTitle icon={Database} title="Пул проектов" />
          <div className="pool-list">
            {Object.entries(projectPool.states ?? {}).map(([state, count]) => (
              <div key={state}>
                <span>{state}</span>
                <strong>{count}</strong>
              </div>
            ))}
            {Object.keys(projectPool.states ?? {}).length === 0 && <div className="empty-state">Пул будет импортирован из OpenStack project scope.</div>}
          </div>
        </div>
      </section>

      <section className="panel audit-panel">
        <PanelTitle icon={Clock3} title="Аудит" right={<span className="notice">{notice}</span>} />
        <div className="audit-table">
          {auditRows.map((event) => (
            <div key={event.id}>
              <span>{formatTime(event.created_at)}</span>
              <code>{event.message_type}</code>
              <StatusPill state={event.state || 'EVENT'} />
              <p>{eventMessage(event)}</p>
            </div>
          ))}
          {auditRows.length === 0 && <div className="empty-state">События появятся после запуска лабораторной.</div>}
        </div>
      </section>
    </>
  )
}

function Metric({ icon: Icon, label, value, hint }) {
  return (
    <div className="metric">
      <Icon size={20} />
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{hint}</small>
    </div>
  )
}

function PanelTitle({ icon: Icon, title, right = null }) {
  return (
    <div className="panel-title">
      <h2><Icon size={19} /> {title}</h2>
      {right}
    </div>
  )
}

function IconButton({ icon: Icon, label, onClick, disabled }) {
  return (
    <button type="button" className="secondary" onClick={onClick} disabled={disabled}>
      <Icon size={17} /> {label}
    </button>
  )
}

function StatusPill({ state }) {
  const normalized = String(state || 'PENDING')
  return <span className={`status ${normalized.toLowerCase()}`}>{normalized}</span>
}

function Bar({ label, value }) {
  const normalized = Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : 0
  return (
    <div className="bar-row">
      <span>{label}</span>
      <div className="bar-track"><div style={{ width: `${normalized}%` }}></div></div>
      <strong>{Number.isFinite(value) ? `${Math.round(value)}%` : 'нет'}</strong>
    </div>
  )
}

function NumberField({ name, label, value, onChange }) {
  return (
    <label className="field" htmlFor={name}>
      <span>{label}</span>
      <input id={name} name={name} type="number" min="1" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  )
}

function TextField({ label, value, onChange, wide = false }) {
  return (
    <label className={`field ${wide ? 'wide' : ''}`}>
      <span>{label}</span>
      <input value={value ?? ''} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function flowClass(current, state) {
  if (!current) {
    return ''
  }
  if (current === 'FAILED' && state === 'FAILED') {
    return 'current failed'
  }
  const currentIndex = statusFlow.indexOf(current)
  const stateIndex = statusFlow.indexOf(state)
  if (stateIndex < currentIndex && current !== 'FAILED') {
    return 'done'
  }
  if (state === current) {
    return 'current'
  }
  return ''
}

function stateLabel(state) {
  return state.split('_').map((part) => <span key={`${state}-${part}`}>{part}</span>)
}

function formatTime(value) {
  if (!value) {
    return 'нет данных'
  }
  return new Date(value).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

function formatDateTime(value) {
  if (!value) {
    return 'нет данных'
  }
  return new Date(value).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatRemaining(ms) {
  if (!Number.isFinite(ms)) {
    return 'нет'
  }
  if (ms <= 0) {
    return '00:00'
  }
  const totalSeconds = Math.floor(ms / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) {
    return `${hours}:${pad2(minutes)}:${pad2(seconds)}`
  }
  return `${pad2(minutes)}:${pad2(seconds)}`
}

function pad2(value) {
  return String(value).padStart(2, '0')
}

function sleep(ms) {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

function capacityFromEvents(events = []) {
  const items = Array.isArray(events) ? events : []
  const event = [...items].reverse().find((item) => item.message_type?.startsWith('evt.capacity.'))
  const payload = event?.payload ?? {}
  const cpuValue = Number(payload.predicted_cpu)
  const ramValue = Number(payload.predicted_ram)
  const storageValue = Number(payload.predicted_storage)
  const threshold = Number(payload.threshold)
  const hasValues = [cpuValue, ramValue, storageValue].some(Number.isFinite)
  return {
    cpuValue,
    ramValue,
    storageValue,
    cpu: Number.isFinite(cpuValue) ? `${Math.round(cpuValue)}%` : 'нет',
    ram: Number.isFinite(ramValue) ? `${Math.round(ramValue)}%` : 'нет',
    storage: Number.isFinite(storageValue) ? `${Math.round(storageValue)}%` : 'нет',
    threshold: Number.isFinite(threshold) ? `${Math.round(threshold)}%` : 'нет',
    label: hasValues ? (payload.approved === false ? 'DENIED' : 'OK') : 'нет данных',
    ok: hasValues && payload.approved !== false,
  }
}

function eventMessage(event) {
  const payload = event.payload ?? {}
  return payload.message || payload.reason || payload.code || payload.state || event.lab_run_id
}

function cleanupDueDate(lab) {
  if (['CLEANING', 'FINISHED', 'FAILED'].includes(lab?.state)) {
    return null
  }
  if (!lab?.cleanup_due_at) {
    return null
  }
  const value = new Date(lab.cleanup_due_at)
  return Number.isNaN(value.getTime()) ? null : value
}

function findRunForDefinition(labs, definition, studentID) {
  const matches = labs.filter((lab) => labMatchesDefinition(lab, definition, studentID))
  return matches.find((lab) => !['FAILED', 'FINISHED'].includes(lab.state)) ?? matches[0] ?? null
}

function labMatchesDefinition(lab, definition, studentID = '') {
  if (!lab || !definition) {
    return false
  }
  if (studentID && lab.student_id !== studentID) {
    return false
  }
  return lab.course_id === definition.course_id && lab.lab_id === definition.lab_id
}

function labKey(lab) {
  if (!lab) {
    return ''
  }
  return `${lab.course_id}:${lab.lab_id}`
}

function normalizeDefinition(definition) {
  return {
    ...emptyLabDefinition,
    ...definition,
    resources: {
      ...emptyLabDefinition.resources,
      ...(definition?.resources ?? {}),
    },
    instances: (definition?.instances?.length ? definition.instances : emptyLabDefinition.instances).map((instance) => ({
      name: instance.name ?? '',
      image_id: instance.image_id ?? '',
      flavor_id: instance.flavor_id ?? '',
      fixed_ip: instance.fixed_ip ?? '',
      disk_gib: Number(instance.disk_gib ?? 0),
    })),
  }
}

function setNestedValue(object, path, value) {
  const [head, ...tail] = path
  if (!head) {
    return object
  }
  if (tail.length === 0) {
    return { ...object, [head]: value }
  }
  return {
    ...object,
    [head]: setNestedValue(object[head] ?? {}, tail, value),
  }
}

export default App
