import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Database,
  HardDrive,
  MonitorUp,
  Network,
  Play,
  RefreshCw,
  Server,
  Settings,
  ShieldCheck,
  Snowflake,
  TerminalSquare,
  Trash2,
} from 'lucide-react'
import './App.css'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const LAB3_COURSE_ID = import.meta.env.VITE_LAB3_COURSE_ID ?? 'course-3'
const LAB3_LAB_ID = import.meta.env.VITE_LAB3_LAB_ID ?? 'lab-3-storage'

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
  'FAILED',
]

const defaultSettings = {
  lab_ttl_seconds: 7200,
  freeze_ttl_seconds: 86400,
  capacity_threshold_percent: 90,
}

const lab3Resources = {
  cpu: 9,
  ram: 16,
  disk: 214,
  instances: 5,
}

function App() {
  const [labs, setLabs] = useState([])
  const [selectedID, setSelectedID] = useState('')
  const [selectedLab, setSelectedLab] = useState(null)
  const [selectedVDI, setSelectedVDI] = useState(null)
  const [projectPool, setProjectPool] = useState({ states: {}, projects: [] })
  const [auditRows, setAuditRows] = useState([])
  const [checkRuns, setCheckRuns] = useState([])
  const [settings, setSettings] = useState(defaultSettings)
  const [studentID, setStudentID] = useState('')
  const [notice, setNotice] = useState('Ожидание данных от API')
  const [loading, setLoading] = useState(false)

  const activeLab = selectedLab ?? labs.find((lab) => lab.id === selectedID) ?? labs[0] ?? null
  const capacity = useMemo(() => capacityFromEvents(activeLab?.events), [activeLab])
  const activeCount = labs.filter((lab) => !['FAILED', 'FINISHED'].includes(lab.state)).length
  const freeProjects = projectPool.states?.FREE ?? 0
  const latestCheck = checkRuns[0]
  const canOperateLab = activeLab && !['CLEANING', 'FAILED', 'FINISHED'].includes(activeLab.state)

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
      setCheckRuns([])
      return
    }
    const [lab, checks, vdi] = await Promise.all([
      requestJSON(`/api/labs/${labRunID}`),
      requestJSON(`/api/labs/${labRunID}/checks?limit=3`).catch(() => ({ runs: [] })),
      requestJSON(`/api/labs/${labRunID}/vdi`).catch(() => null),
    ])
    setSelectedLab(lab)
    setSelectedVDI(vdi)
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
      const nextLabs = await loadLabs()
      await Promise.all([loadProjectPool(), loadSettings(), loadAudit()])
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
  }, [loadAudit, loadLabs, loadProjectPool, loadSelectedDetails, loadSettings, selectedID])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      refreshAll()
    }, 0)
    return () => window.clearTimeout(timer)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const timer = window.setTimeout(() => {
      loadSelectedDetails(selectedID).catch((error) => setNotice(`Ошибка стенда: ${error.message}`))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [loadSelectedDetails, selectedID])

  useEffect(() => {
    if (!selectedID || typeof EventSource === 'undefined') {
      return undefined
    }
    const source = new EventSource(`${API_BASE}/api/labs/${selectedID}/events`)
    source.addEventListener('lab_run', (event) => {
      const payload = JSON.parse(event.data)
      setNotice(`Live update: ${payload.state}`)
      loadSelectedDetails(selectedID).catch((error) => setNotice(`Ошибка live update: ${error.message}`))
      loadLabs().catch(() => {})
    })
    source.onerror = () => {
      source.close()
    }
    return () => source.close()
  }, [loadLabs, loadSelectedDetails, selectedID])

  async function sendCommand(path, body = {}) {
    setNotice(`Команда отправляется: ${path}`)
    try {
      const payload = await requestJSON(path, {
        method: 'POST',
        body: JSON.stringify(body),
      })
      setNotice(`Команда принята: ${payload.command_id ?? payload.lab_run_id}`)
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

  function requestLab3() {
    const normalizedStudentID = studentID.trim()
    if (!normalizedStudentID) {
      setNotice('Укажи student_id перед запуском лабораторной 3')
      return
    }
    sendCommand('/api/labs', {
      student_id: normalizedStudentID,
      course_id: LAB3_COURSE_ID,
      lab_id: LAB3_LAB_ID,
      source: 'ui',
      idempotency_key: `lab3:${normalizedStudentID}:${Date.now()}`,
    })
  }

  function freezeLab() {
    if (!activeLab) return
    sendCommand(`/api/labs/${activeLab.id}/freeze`, {
      reason: 'support_freeze',
      idempotency_key: `freeze:${activeLab.id}:${Date.now()}`,
    })
  }

  function cleanupLab() {
    if (!activeLab) return
    sendCommand(`/api/labs/${activeLab.id}/cleanup`, {
      reason: 'teacher_cleanup',
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

  function saveSettings() {
    sendCommand('/api/admin/settings', {
      changed_by: 'teacher-console',
      values: settings,
      idempotency_key: `settings:${Date.now()}`,
    })
  }

  return (
    <div className="app-shell">
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
        <nav className="topnav" aria-label="Основная навигация">
          <a href="#monitoring">Мониторинг</a>
          <a href="#labs">Стенды</a>
          <a href="#pool">Пул проектов</a>
          <a href="#capacity">Capacity</a>
          <a href="#checks">Проверки</a>
          <a href="#audit">Аудит</a>
        </nav>
      </header>

      <main>
        <section className="summary-band" id="monitoring">
          <div>
            <p className="eyebrow">Лабораторная 3 · OpenStack · NATS</p>
            <h1>Консоль управления лабораторными стендами</h1>
          </div>
          <div className="summary-actions">
            <label className="student-field">
              <span>student_id</span>
              <input id="student_id" name="student_id" value={studentID} onChange={(event) => setStudentID(event.target.value)} placeholder="moodle:42" />
            </label>
            <button type="button" className="primary" onClick={requestLab3} disabled={loading}>
              <Play size={17} /> Запустить лабораторную 3
            </button>
            <button type="button" className="secondary" onClick={refreshAll} disabled={loading}>
              <RefreshCw size={17} /> Обновить
            </button>
          </div>
        </section>

        <section className="metrics-grid" aria-label="Ключевые метрики">
          <Metric icon={Activity} label="Активные стенды" value={activeCount} hint={`${labs.length} всего в read model`} />
          <Metric icon={Database} label="Свободные проекты" value={freeProjects} hint={`${projectPool.projects?.length ?? 0} реальных проектов`} />
          <Metric icon={HardDrive} label="Storage forecast" value={capacity.storage} hint={`threshold ${capacity.threshold}`} />
          <Metric icon={ShieldCheck} label="Проверки SSH" value={latestCheck?.state ?? 'нет'} hint={latestCheck ? `profile ${latestCheck.profile_id}` : 'нет запусков'} />
        </section>

        <section className="workspace">
          <div className="panel labs-panel" id="labs">
            <PanelTitle icon={MonitorUp} title="Стенды" right={<StatusPill state={activeLab?.state ?? 'EMPTY'} />} />
            <div className="lab-list">
              {labs.length === 0 && <div className="empty-state">В read model пока нет лабораторных запусков.</div>}
              {labs.map((lab) => (
                <button
                  key={lab.id}
                  type="button"
                  className={`lab-row ${lab.id === activeLab?.id ? 'active' : ''}`}
                  onClick={() => setSelectedID(lab.id)}
                >
                  <span>
                    <strong>{lab.student_id}</strong>
                    <small>{lab.course_id} · {lab.lab_id}</small>
                  </span>
                  <StatusPill state={lab.state} />
                </button>
              ))}
            </div>
          </div>

          <div className="panel detail-panel">
            <PanelTitle icon={Network} title="State machine" right={<span className="muted">{formatTime(activeLab?.updated_at)}</span>} />
            <div className="flow">
              {statusFlow.map((state) => (
                <div key={state} className={`flow-step ${flowClass(activeLab?.state, state)}`}>
                  <span></span>
                  <small>{stateLabel(state)}</small>
                </div>
              ))}
            </div>

            <div className="detail-grid">
              <div className="resource-map" aria-label="Топология лабораторной 3">
                <div className="node moodle">Moodle</div>
                <div className="node gateway">Gateway</div>
                <div className="node vm">5 VM</div>
                <div className="node storage">Storage</div>
              </div>
              <div className="resource-table">
                <div><span>vCPU</span><strong>{lab3Resources.cpu}</strong></div>
                <div><span>RAM</span><strong>{lab3Resources.ram} GiB</strong></div>
                <div><span>Disk</span><strong>{lab3Resources.disk} GiB</strong></div>
                <div><span>VDI</span><strong>{selectedVDI?.available ? 'issued' : 'pending'}</strong></div>
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

            <div className="toolbar" id="checks">
              <button type="button" className="primary" disabled={!selectedVDI?.available} onClick={() => window.open(selectedVDI.url, '_blank')}>
                <MonitorUp size={17} /> VDI
              </button>
              <button type="button" className="secondary" disabled={!canOperateLab} onClick={freezeLab}>
                <Snowflake size={17} /> Freeze
              </button>
              <button type="button" className="secondary" disabled={!canOperateLab} onClick={checkLab}>
                <TerminalSquare size={17} /> Check
              </button>
              <button type="button" className="danger" disabled={!canOperateLab} onClick={cleanupLab}>
                <Trash2 size={17} /> Cleanup
              </button>
            </div>
          </div>
        </section>

        <section className="operations-grid">
          <div className="panel" id="capacity">
            <PanelTitle icon={Activity} title="Capacity" right={<span className={capacity.ok ? 'ok' : 'muted'}>{capacity.label}</span>} />
            <div className="capacity-bars">
              <Bar label="CPU" value={capacity.cpuValue} />
              <Bar label="RAM" value={capacity.ramValue} />
              <Bar label="Storage" value={capacity.storageValue} />
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

          <div className="panel settings-panel">
            <PanelTitle icon={Settings} title="Настройки" />
            <NumberField name="lab_ttl_seconds" label="Lab TTL, sec" value={settings.lab_ttl_seconds} onChange={(value) => setSettings({ ...settings, lab_ttl_seconds: value })} />
            <NumberField name="freeze_ttl_seconds" label="Freeze TTL, sec" value={settings.freeze_ttl_seconds} onChange={(value) => setSettings({ ...settings, freeze_ttl_seconds: value })} />
            <NumberField name="capacity_threshold_percent" label="Capacity threshold, %" value={settings.capacity_threshold_percent} onChange={(value) => setSettings({ ...settings, capacity_threshold_percent: value })} />
            <button type="button" className="primary full" onClick={saveSettings}>
              <CheckCircle2 size={17} /> Применить
            </button>
          </div>

          <div className="panel checks-panel">
            <PanelTitle icon={TerminalSquare} title="SSH-проверка" right={<StatusPill state={latestCheck?.state ?? 'PENDING'} />} />
            <div className="check-meta">
              <span>Профиль</span>
              <strong>{latestCheck?.profile_id ?? 'default'}</strong>
            </div>
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
        </section>

        <section className="panel audit-panel" id="audit">
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
      </main>
    </div>
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
    <label className="number-field" htmlFor={name}>
      <span>{label}</span>
      <input id={name} name={name} type="number" min="1" value={value} onChange={(event) => onChange(Number(event.target.value))} />
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

export default App
