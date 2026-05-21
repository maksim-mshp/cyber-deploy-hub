import { useEffect, useMemo, useState } from 'react'
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

const initialLabs = [
  {
    id: '33333333-3333-4333-8333-333333333333',
    student: 'ivan.petrov',
    course: 'Linux administration',
    lab: 'LAB-02',
    state: 'READY',
    vdi: '/vdi/session/demo-ready',
    cpu: 16,
    ram: 24,
    disk: 214,
    updated: '12:18',
    incident: null,
  },
  {
    id: '44444444-4444-4444-8444-444444444444',
    student: 'anna.sidorova',
    course: 'Network services',
    lab: 'LAB-01',
    state: 'DEPLOYING',
    vdi: '',
    cpu: 12,
    ram: 18,
    disk: 140,
    updated: '12:11',
    incident: null,
  },
  {
    id: '55555555-5555-4555-8555-555555555555',
    student: 'moodle.user42',
    course: 'Windows domain',
    lab: 'LAB-05',
    state: 'FAILED',
    vdi: '',
    cpu: 20,
    ram: 32,
    disk: 260,
    updated: '11:54',
    incident: {
      code: 'CAPACITY_DENIED',
      stage: 'CHECKING_CAPACITY',
      correlation: '9c1b2a8e',
      action: 'Освободить ресурсы или поднять threshold после проверки кластера',
    },
  },
]

const statusFlow = [
  'REQUESTED',
  'ALLOCATING_PROJECT',
  'CHECKING_CAPACITY',
  'DEPLOYING',
  'ISSUING_VDI_ACCESS',
  'READY',
  'FROZEN',
  'CLEANING',
  'FAILED',
]

const auditRows = [
  ['12:18', 'evt.vdi.access_issued.v1', 'READY', 'VDI token issued'],
  ['12:16', 'evt.lifecycle.cleanup_scheduled.v1', 'READY', 'Cleanup at 14:16'],
  ['12:11', 'evt.cloud.vdi_deployed.v1', 'DEPLOYING', '5 VM instances active'],
  ['11:54', 'evt.capacity.denied.v1', 'FAILED', 'Predicted storage exceeds 90%'],
]

const projectStates = [
  ['FREE', 18],
  ['ALLOCATED', 7],
  ['QUARANTINED', 1],
]

function App() {
  const [labs, setLabs] = useState(initialLabs)
  const [selectedID, setSelectedID] = useState(initialLabs[0].id)
  const [notice, setNotice] = useState('API gateway готов к командам')
  const [settings, setSettings] = useState({
    lab_ttl_seconds: 7200,
    freeze_ttl_seconds: 86400,
    capacity_threshold_percent: 90,
  })

  const selectedLab = useMemo(
    () => labs.find((lab) => lab.id === selectedID) ?? labs[0],
    [labs, selectedID],
  )

  useEffect(() => {
    if (!selectedID || typeof EventSource === 'undefined') {
      return undefined
    }
    const source = new EventSource(`${API_BASE}/api/labs/${selectedID}/events`)
    source.addEventListener('lab_run', (event) => {
      const payload = JSON.parse(event.data)
      setNotice(`Live update: ${payload.state}`)
      setLabs((items) =>
        items.map((lab) =>
          lab.id === selectedID
            ? { ...lab, state: payload.state, updated: new Date(payload.created_at).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) }
            : lab,
        ),
      )
    })
    source.onerror = () => {
      source.close()
    }
    return () => source.close()
  }, [selectedID])

  async function sendCommand(path, body = {}) {
    setNotice(`Команда отправляется: ${path}`)
    try {
      const response = await fetch(`${API_BASE}${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error?.message ?? response.statusText)
      }
      const payload = await response.json()
      setNotice(`Команда принята: ${payload.command_id ?? payload.lab_run_id}`)
    } catch (error) {
      setNotice(`Локальный режим: ${error.message}`)
    }
  }

  function updateLabState(id, state) {
    setLabs((items) =>
      items.map((lab) =>
        lab.id === id
          ? { ...lab, state, updated: new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) }
          : lab,
      ),
    )
  }

  function freezeLab() {
    updateLabState(selectedLab.id, 'FROZEN')
    sendCommand(`/api/labs/${selectedLab.id}/freeze`, {
      reason: 'support_freeze',
      idempotency_key: `freeze:${selectedLab.id}`,
    })
  }

  function cleanupLab() {
    updateLabState(selectedLab.id, 'CLEANING')
    sendCommand(`/api/labs/${selectedLab.id}/cleanup`, {
      reason: 'teacher_cleanup',
      idempotency_key: `cleanup:${selectedLab.id}`,
    })
  }

  function checkLab() {
    updateLabState(selectedLab.id, 'VERIFYING')
    sendCommand(`/api/labs/${selectedLab.id}/check`, {
      profile_id: 'default',
      idempotency_key: `check:${selectedLab.id}`,
    })
  }

  function requestLab() {
    sendCommand('/api/labs', {
      student_id: 'demo.student',
      course_id: 'course-linux',
      lab_id: 'LAB-02',
      source: 'ui',
      idempotency_key: `ui:${Date.now()}`,
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
            <p className="eyebrow">VDI, OpenStack, Moodle, NATS</p>
            <h1>Консоль управления лабораторными стендами</h1>
          </div>
          <div className="summary-actions">
            <button type="button" className="primary" onClick={requestLab}>
              <Play size={17} /> Запустить стенд
            </button>
            <button type="button" className="secondary" onClick={() => setNotice('Статусы обновлены из read model')}>
              <RefreshCw size={17} /> Обновить
            </button>
          </div>
        </section>

        <section className="metrics-grid" aria-label="Ключевые метрики">
          <Metric icon={Activity} label="Активные стенды" value="26" hint="7 в деплое" />
          <Metric icon={Database} label="Свободные проекты" value="18" hint="1 в карантине" />
          <Metric icon={HardDrive} label="Storage forecast" value="74%" hint="threshold 90%" />
          <Metric icon={ShieldCheck} label="Проверки SSH" value="91%" hint="успешных за день" />
        </section>

        <section className="workspace">
          <div className="panel labs-panel" id="labs">
            <PanelTitle icon={MonitorUp} title="Стенды" right={<StatusPill state={selectedLab.state} />} />
            <div className="lab-list">
              {labs.map((lab) => (
                <button
                  key={lab.id}
                  type="button"
                  className={`lab-row ${lab.id === selectedID ? 'active' : ''}`}
                  onClick={() => setSelectedID(lab.id)}
                >
                  <span>
                    <strong>{lab.student}</strong>
                    <small>{lab.course} · {lab.lab}</small>
                  </span>
                  <StatusPill state={lab.state} />
                </button>
              ))}
            </div>
          </div>

          <div className="panel detail-panel">
            <PanelTitle icon={Network} title="State machine" right={<span className="muted">{selectedLab.updated}</span>} />
            <div className="flow">
              {statusFlow.map((state) => (
                <div key={state} className={`flow-step ${flowClass(selectedLab.state, state)}`}>
                  <span></span>
                  <small>{stateLabel(state)}</small>
                </div>
              ))}
            </div>

            <div className="detail-grid">
              <div className="resource-map" aria-label="Топология стенда">
                <div className="node moodle">Moodle</div>
                <div className="node gateway">Gateway</div>
                <div className="node vm">VDI VM</div>
                <div className="node storage">Storage</div>
              </div>
              <div className="resource-table">
                <div><span>vCPU</span><strong>{selectedLab.cpu}</strong></div>
                <div><span>RAM</span><strong>{selectedLab.ram} GiB</strong></div>
                <div><span>Disk</span><strong>{selectedLab.disk} GiB</strong></div>
                <div><span>VDI</span><strong>{selectedLab.vdi ? 'issued' : 'pending'}</strong></div>
              </div>
            </div>

            {selectedLab.incident && (
              <div className="incident">
                <AlertTriangle size={19} />
                <div>
                  <strong>{selectedLab.incident.code}</strong>
                  <span>{selectedLab.incident.stage} · correlation {selectedLab.incident.correlation}</span>
                  <p>{selectedLab.incident.action}</p>
                </div>
              </div>
            )}

            <div className="toolbar" id="checks">
              <button type="button" className="primary" disabled={!selectedLab.vdi} onClick={() => window.open(selectedLab.vdi, '_blank')}>
                <MonitorUp size={17} /> VDI
              </button>
              <button type="button" className="secondary" onClick={freezeLab}>
                <Snowflake size={17} /> Freeze
              </button>
              <button type="button" className="secondary" onClick={checkLab}>
                <TerminalSquare size={17} /> Check
              </button>
              <button type="button" className="danger" onClick={cleanupLab}>
                <Trash2 size={17} /> Cleanup
              </button>
            </div>
          </div>
        </section>

        <section className="operations-grid">
          <div className="panel" id="capacity">
            <PanelTitle icon={Activity} title="Capacity" right={<span className="ok">OK</span>} />
            <div className="capacity-bars">
              <Bar label="CPU" value={62} />
              <Bar label="RAM" value={71} />
              <Bar label="Storage" value={74} />
            </div>
          </div>

          <div className="panel" id="pool">
            <PanelTitle icon={Database} title="Пул проектов" />
            <div className="pool-list">
              {projectStates.map(([state, count]) => (
                <div key={state}>
                  <span>{state}</span>
                  <strong>{count}</strong>
                </div>
              ))}
            </div>
          </div>

          <div className="panel settings-panel">
            <PanelTitle icon={Settings} title="Настройки" />
            <NumberField label="Lab TTL, sec" value={settings.lab_ttl_seconds} onChange={(value) => setSettings({ ...settings, lab_ttl_seconds: value })} />
            <NumberField label="Freeze TTL, sec" value={settings.freeze_ttl_seconds} onChange={(value) => setSettings({ ...settings, freeze_ttl_seconds: value })} />
            <NumberField label="Capacity threshold, %" value={settings.capacity_threshold_percent} onChange={(value) => setSettings({ ...settings, capacity_threshold_percent: value })} />
            <button type="button" className="primary full" onClick={saveSettings}>
              <CheckCircle2 size={17} /> Применить
            </button>
          </div>
        </section>

        <section className="panel audit-panel" id="audit">
          <PanelTitle icon={Clock3} title="Аудит" right={<span className="notice">{notice}</span>} />
          <div className="audit-table">
            {auditRows.map(([time, event, state, message]) => (
              <div key={`${time}-${event}`}>
                <span>{time}</span>
                <code>{event}</code>
                <StatusPill state={state} />
                <p>{message}</p>
              </div>
            ))}
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
  return <span className={`status ${state.toLowerCase()}`}>{state}</span>
}

function Bar({ label, value }) {
  return (
    <div className="bar-row">
      <span>{label}</span>
      <div className="bar-track"><div style={{ width: `${value}%` }}></div></div>
      <strong>{value}%</strong>
    </div>
  )
}

function NumberField({ label, value, onChange }) {
  return (
    <label className="number-field">
      <span>{label}</span>
      <input type="number" min="1" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  )
}

function flowClass(current, state) {
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

export default App
