import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  CheckCircle2,
  CircleAlert,
  Cloud,
  DoorOpen,
  Loader2,
  Lock,
  LogOut,
  Play,
  Plus,
  RefreshCw,
  Save,
  Server,
  Trash2,
} from 'lucide-react'
import './App.css'

const activeStates = new Set([
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
])

const terminalStates = new Set(['FINISHED', 'FAILED'])

const emptyDefinition = {
  course_id: 'course-3',
  lab_id: '',
  title: '',
  description: '',
  enabled: true,
  resources: { vcpu: 1, ram_mib: 2048, disk_gib: 20 },
  instances: [{ name: 'vm-1', image_id: '', flavor_id: '', fixed_ip: '', disk_gib: 20 }],
}

const newDefinitionKey = '__new_definition__'

const defaultRuntimeSettings = {
  lab_ttl_minutes: 120,
  freeze_ttl_hours: 24,
  capacity_threshold_percent: 90,
}

const emptyProjectPool = {
  states: {},
  projects: [],
}

const projectSeedTemplate = JSON.stringify(
  {
    domains: [{ domain_id: '', course_id: 'course-3', name: '' }],
    projects: [{ project_id: '', domain_id: '', name: '' }],
  },
  null,
  2,
)

let launchNoticeCache

function App() {
  const [user, setUser] = useState(null)
  const [booting, setBooting] = useState(true)
  const [notice, setNotice] = useState('')
  const [launchNotice] = useState(getInitialLaunchNotice)

  useEffect(() => {
    requestJSON('/api/auth/me')
      .then((data) => setUser(data.user))
      .catch(() => setUser(null))
      .finally(() => setBooting(false))
  }, [])

  async function login(credentials) {
    const data = await requestJSON('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify(credentials),
    })
    setUser(data.user)
    setNotice('')
  }

  async function logout() {
    await requestJSON('/api/auth/logout', { method: 'POST' }).catch(() => null)
    setUser(null)
  }

  if (booting) {
    return <Splash />
  }

  if (!user) {
    return <LoginScreen login={login} notice={notice} setNotice={setNotice} />
  }

  return (
    <Shell user={user} logout={logout}>
      {user.role === 'teacher' ? <TeacherDashboard user={user} /> : <StudentDashboard user={user} initialNotice={launchNotice} />}
    </Shell>
  )
}

function Splash() {
  return (
    <main className="auth-page">
      <div className="auth-panel compact">
        <Loader2 className="spin" size={28} />
      </div>
    </main>
  )
}

function LoginScreen({ login, notice, setNotice }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event) {
    event.preventDefault()
    setBusy(true)
    setNotice('')
    try {
      await login({ username, password })
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="auth-page">
      <form className="auth-panel" onSubmit={submit}>
        <div className="brand-mark">
          <Cloud size={28} />
        </div>
        <div>
          <p className="eyebrow">Cyber Deploy Hub</p>
          <h1>Вход</h1>
        </div>
        <label>
          <span>Логин</span>
          <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
        </label>
        <label>
          <span>Пароль</span>
          <input
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            type="password"
            autoComplete="current-password"
          />
        </label>
        {notice ? <p className="form-error">{notice}</p> : null}
        <button className="primary-button" type="submit" disabled={busy || !username.trim() || !password}>
          {busy ? <Loader2 className="spin" size={18} /> : <Lock size={18} />}
          Войти
        </button>
      </form>
    </main>
  )
}

function Shell({ user, logout, children }) {
  return (
    <>
      <header className="topbar">
        <div>
          <strong>Cyber Deploy Hub</strong>
          <span>{user.role === 'teacher' ? 'Преподаватель' : 'Студент'}</span>
        </div>
        <div className="topbar-actions">
          <span>{user.display_name || user.subject}</span>
          <button className="icon-button" type="button" onClick={logout} aria-label="Выйти" title="Выйти">
            <LogOut size={18} />
          </button>
        </div>
      </header>
      <main className="workspace">{children}</main>
    </>
  )
}

function StudentDashboard({ user, initialNotice = '' }) {
  const [definitions, setDefinitions] = useState([])
  const [runs, setRuns] = useState([])
  const [selectedKey, setSelectedKey] = useState('')
  const [selectedRunID, setSelectedRunID] = useState('')
  const [instanceState, setInstanceState] = useState({ runID: '', items: [] })
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState(initialNotice)

  const activeRun = useMemo(() => runs.find((run) => activeStates.has(run.state)) || null, [runs])
  const selectedDefinition = definitions.find((lab) => labKey(lab) === selectedKey) || definitions[0] || null
  const selectedRun = activeRun || runs.find((run) => run.id === selectedRunID) || runs[0] || null
  const selectedRunInstanceID = selectedRun?.id || ''
  const selectedRunInstanceRevision = selectedRun ? `${selectedRun.state}:${selectedRun.updated_at}` : ''
  const instances = instanceState.runID === selectedRunInstanceID ? instanceState.items : []

  const applySnapshot = useCallback((snapshot) => {
    setDefinitions(snapshot.definitions)
    setRuns(snapshot.runs)
    setSelectedKey((current) => current || (snapshot.definitions[0] ? labKey(snapshot.definitions[0]) : ''))
    setSelectedRunID((current) => current || snapshot.runs[0]?.id || '')
  }, [])

  const refresh = useCallback(async () => {
    applySnapshot(await loadStudentSnapshot())
  }, [applySnapshot])

  useEffect(() => {
    let cancelled = false
    const load = () => {
      loadStudentSnapshot()
        .then((snapshot) => {
          if (!cancelled) {
            applySnapshot(snapshot)
          }
        })
        .catch((error) => {
          if (!cancelled) {
            setNotice(error.message)
          }
        })
    }
    load()
    const timer = window.setInterval(load, 5000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [applySnapshot])

  useEffect(() => {
    if (!selectedRunInstanceID) {
      return
    }
    let cancelled = false
    requestJSON(`/api/labs/${selectedRunInstanceID}/instances`)
      .then((data) => {
        if (!cancelled) {
          setInstanceState({ runID: selectedRunInstanceID, items: data.instances ?? [] })
        }
      })
      .catch(() => {
        if (!cancelled) {
          setInstanceState({ runID: selectedRunInstanceID, items: [] })
        }
      })
    return () => {
      cancelled = true
    }
  }, [selectedRunInstanceID, selectedRunInstanceRevision])

  async function startLab() {
    if (!selectedDefinition || activeRun) {
      return
    }
    setBusy(true)
    setNotice('')
    try {
      const accepted = await requestJSON('/api/labs', {
        method: 'POST',
        body: JSON.stringify({
          course_id: selectedDefinition.course_id,
          lab_id: selectedDefinition.lab_id,
          idempotency_key: `student:${user.subject}:${selectedDefinition.course_id}:${selectedDefinition.lab_id}:${Date.now()}`,
        }),
      })
      setSelectedRunID(accepted.lab_run_id)
      await refresh()
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  async function finishLab() {
    if (!selectedRun || selectedRun.state === 'FINISHED' || selectedRun.state === 'CLEANING') {
      return
    }
    setBusy(true)
    setNotice('')
    try {
      await requestJSON(`/api/labs/${selectedRun.id}/cleanup`, {
        method: 'POST',
        body: JSON.stringify({
          reason: 'student_cleanup',
          idempotency_key: `student-cleanup:${selectedRun.id}:${Date.now()}`,
        }),
      })
      await refresh()
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="student-layout">
      <section className="hero-band">
        <div>
          <p className="eyebrow">Лабораторные работы</p>
          <h1>{activeRun ? 'Текущий стенд' : 'Выбор лабораторной'}</h1>
        </div>
        <button className="ghost-button" type="button" onClick={refresh}>
          <RefreshCw size={18} />
          Обновить
        </button>
      </section>

      {notice ? <Notice>{notice}</Notice> : null}

      {!activeRun ? (
        <section className="launch-panel">
          <label>
            <span>Лабораторная работа</span>
            <select value={selectedKey} onChange={(event) => setSelectedKey(event.target.value)}>
              {definitions.map((lab) => (
                <option key={labKey(lab)} value={labKey(lab)}>
                  {lab.title}
                </option>
              ))}
            </select>
          </label>
          {selectedDefinition ? <LabDefinitionSummary definition={selectedDefinition} /> : null}
          <button className="primary-button" type="button" onClick={startLab} disabled={busy || !selectedDefinition}>
            {busy ? <Loader2 className="spin" size={18} /> : <Play size={18} />}
            Развернуть
          </button>
        </section>
      ) : null}

      {selectedRun ? (
        <RunPanel
          run={selectedRun}
          instances={instances}
          title={definitionTitle(selectedRun, definitions)}
          onFinish={finishLab}
          finishing={busy}
          compact
        />
      ) : null}
    </div>
  )
}

function TeacherDashboard({ user }) {
  const [definitions, setDefinitions] = useState([])
  const [runs, setRuns] = useState([])
  const [images, setImages] = useState([])
  const [flavors, setFlavors] = useState([])
  const [selectedLabKey, setSelectedLabKey] = useState('')
  const [draft, setDraft] = useState(emptyDefinition)
  const [selectedRunID, setSelectedRunID] = useState('')
  const [instanceState, setInstanceState] = useState({ runID: '', items: [] })
  const [notice, setNotice] = useState('')
  const [catalogWarning, setCatalogWarning] = useState('')
  const [runtimeSettings, setRuntimeSettings] = useState(defaultRuntimeSettings)
  const [projectPool, setProjectPool] = useState(emptyProjectPool)
  const [busy, setBusy] = useState(false)

  const activeRuns = useMemo(() => runs.filter((run) => activeStates.has(run.state)), [runs])
  const selectedRun = activeRuns.find((run) => run.id === selectedRunID) || activeRuns[0] || null
  const selectedDefinition = definitions.find((lab) => labKey(lab) === selectedLabKey) || null
  const selectedRunInstanceID = selectedRun?.id || ''
  const selectedRunInstanceRevision = selectedRun ? `${selectedRun.state}:${selectedRun.updated_at}` : ''
  const instances = instanceState.runID === selectedRunInstanceID ? instanceState.items : []

  const applySnapshot = useCallback((snapshot) => {
    setDefinitions(snapshot.definitions)
    setRuns(snapshot.runs)
    setSelectedLabKey((current) => {
      if (current === newDefinitionKey) {
        return current
      }
      if (current && snapshot.definitions.some((definition) => labKey(definition) === current)) {
        return current
      }
      return snapshot.definitions[0] ? labKey(snapshot.definitions[0]) : ''
    })
    setDraft((current) => {
      if (current.lab_id) {
        return current
      }
      return snapshot.definitions[0] ? cloneDefinition(snapshot.definitions[0]) : current
    })
    setSelectedRunID((current) => {
      const stillActive = snapshot.runs.some((run) => run.id === current && activeStates.has(run.state))
      if (stillActive) {
        return current
      }
      return snapshot.runs.find((run) => activeStates.has(run.state))?.id || ''
    })
    setRuntimeSettings(snapshot.settings)
    setProjectPool(snapshot.projectPool)
  }, [])

  const refresh = useCallback(async () => {
    applySnapshot(await loadTeacherSnapshot())
  }, [applySnapshot])

  useEffect(() => {
    let cancelled = false
    const load = () => {
      loadTeacherSnapshot()
        .then((snapshot) => {
          if (!cancelled) {
            applySnapshot(snapshot)
          }
        })
        .catch((error) => {
          if (!cancelled) {
            setNotice(error.message)
          }
        })
    }
    load()
    Promise.allSettled([
      requestJSON('/api/teacher/openstack/images'),
      requestJSON('/api/teacher/openstack/flavors'),
    ]).then(([imageResult, flavorResult]) => {
      if (!cancelled) {
        const warnings = []
        if (imageResult.status === 'fulfilled') {
          setImages(imageResult.value.images ?? [])
        } else {
          setImages([])
          warnings.push('images')
        }
        if (flavorResult.status === 'fulfilled') {
          setFlavors(flavorResult.value.flavors ?? [])
        } else {
          setFlavors([])
          warnings.push('flavors')
        }
        setCatalogWarning(warnings.length ? `Каталог OpenStack недоступен: ${warnings.join(', ')}` : '')
      }
    })
    const timer = window.setInterval(load, 5000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [applySnapshot])

  useEffect(() => {
    if (!selectedRunInstanceID) {
      return
    }
    let cancelled = false
    requestJSON(`/api/labs/${selectedRunInstanceID}/instances`)
      .then((data) => {
        if (!cancelled) {
          setInstanceState({ runID: selectedRunInstanceID, items: data.instances ?? [] })
        }
      })
      .catch(() => {
        if (!cancelled) {
          setInstanceState({ runID: selectedRunInstanceID, items: [] })
        }
      })
    return () => {
      cancelled = true
    }
  }, [selectedRunInstanceID, selectedRunInstanceRevision])

  async function saveDefinition() {
    setBusy(true)
    setNotice('')
    try {
      const payload = normalizeDraft(draft)
      const validationError = validateDraft(payload)
      if (validationError) {
        throw new Error(validationError)
      }
      const saved = await requestJSON('/api/teacher/lab-definitions', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      setSelectedLabKey(labKey(saved.lab))
      setDraft(cloneDefinition(saved.lab))
      await refresh()
      setNotice('Конфигурация сохранена')
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  async function launchPreset() {
    const lab = selectedDefinition
    if (!lab) {
      return
    }
    setBusy(true)
    setNotice('')
    try {
      const accepted = await requestJSON('/api/labs', {
        method: 'POST',
        body: JSON.stringify({
          course_id: lab.course_id,
          lab_id: lab.lab_id,
          student_id: `teacher:${user.subject}`,
          source: 'teacher-ui',
          idempotency_key: `teacher:${user.subject}:${lab.course_id}:${lab.lab_id}:${Date.now()}`,
        }),
      })
      setSelectedRunID(accepted.lab_run_id)
      await refresh()
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  async function runAction(path, body = {}) {
    if (!selectedRun) {
      return
    }
    setBusy(true)
    setNotice('')
    try {
      await requestJSON(`/api/labs/${selectedRun.id}/${path}`, {
        method: 'POST',
        body: JSON.stringify(body),
      })
      await refresh()
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  async function saveRuntimeSettings(nextSettings) {
    setBusy(true)
    setNotice('')
    try {
      await requestJSON('/api/admin/settings', {
        method: 'POST',
        body: JSON.stringify({
          changed_by: user.subject,
          values: runtimeSettingsPayload(nextSettings),
          idempotency_key: `settings:${user.subject}:${Date.now()}`,
        }),
      })
      await refresh()
      setRuntimeSettings(nextSettings)
      setNotice('Настройки сохранены')
    } catch (error) {
      setNotice(error.message)
    } finally {
      setBusy(false)
    }
  }

  async function importProjectPool(seed) {
    setBusy(true)
    setNotice('')
    try {
      await requestJSON('/api/admin/project-pool/import', {
        method: 'POST',
        body: JSON.stringify({
          ...seed,
          idempotency_key: `project-pool:${user.subject}:${Date.now()}`,
        }),
      })
      await refresh()
      setNotice('Импорт пула принят')
    } catch (error) {
      setNotice(error.message)
      throw error
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="teacher-layout">
      <section className="hero-band">
        <div>
          <p className="eyebrow">Teacher Workspace</p>
          <h1>Управление стендами</h1>
        </div>
        <button className="ghost-button" type="button" onClick={refresh}>
          <RefreshCw size={18} />
          Обновить
        </button>
      </section>

      {notice ? <Notice>{notice}</Notice> : null}
      {catalogWarning ? <Notice tone="danger">{catalogWarning}</Notice> : null}

      <section className="metrics-row">
        <Metric icon={Activity} label="Активные стенды" value={activeRuns.length} />
        <Metric icon={Server} label="Конфигурации" value={definitions.length} />
        <Metric icon={Cloud} label="FREE projects" value={projectStateCount(projectPool.states, 'FREE')} />
        <Metric icon={Cloud} label="OpenStack images" value={images.length} />
      </section>

      <RuntimeSettingsPanel
        key={`${runtimeSettings.lab_ttl_minutes}:${runtimeSettings.freeze_ttl_hours}:${runtimeSettings.capacity_threshold_percent}`}
        settings={runtimeSettings}
        onSave={saveRuntimeSettings}
        busy={busy}
      />

      <ProjectPoolPanel pool={projectPool} onImport={importProjectPool} busy={busy} />

      <div className="teacher-grid">
        <section className="panel">
          <div className="panel-header">
            <h2>Активные стенды</h2>
          </div>
          <div className="run-list">
            {activeRuns.map((run) => (
              <button
                key={run.id}
                className={selectedRun?.id === run.id ? 'run-item selected' : 'run-item'}
                type="button"
                onClick={() => setSelectedRunID(run.id)}
              >
                <span>{run.student_id}</span>
                <strong>{definitionTitle(run, definitions)}</strong>
                <StateBadge state={run.state} />
              </button>
            ))}
            {activeRuns.length === 0 ? <p className="empty">Активных стендов нет</p> : null}
          </div>
        </section>

        <section className="panel">
          <div className="panel-header">
            <h2>Пресеты</h2>
            <button
              className="icon-button"
              type="button"
              onClick={() => {
                setSelectedLabKey(newDefinitionKey)
                setDraft(createNewDefinitionDraft(definitions, images, flavors))
              }}
              title="Новая"
            >
              <Plus size={18} />
            </button>
          </div>
          <label>
            <span>Конфигурация</span>
            <select
              value={selectedLabKey}
              onChange={(event) => {
                const nextKey = event.target.value
                setSelectedLabKey(nextKey)
                if (nextKey === newDefinitionKey) {
                  setDraft(createNewDefinitionDraft(definitions, images, flavors))
                  return
                }
                const lab = definitions.find((item) => labKey(item) === nextKey)
                if (lab) {
                  setDraft(cloneDefinition(lab))
                }
              }}
            >
              {selectedLabKey === newDefinitionKey ? <option value={newDefinitionKey}>Новая конфигурация</option> : null}
              {definitions.map((lab) => (
                <option key={labKey(lab)} value={labKey(lab)}>
                  {lab.title} {lab.enabled ? '' : '(teacher-only)'}
                </option>
              ))}
            </select>
          </label>
          <button className="secondary-button" type="button" onClick={launchPreset} disabled={busy || !selectedDefinition}>
            <Play size={18} />
            Запустить пресет
          </button>
        </section>
      </div>

      {selectedRun ? (
        <RunPanel
          run={selectedRun}
          instances={instances}
          title={definitionTitle(selectedRun, definitions)}
          onFinish={() => runAction('cleanup', { reason: 'teacher_cleanup', idempotency_key: `teacher-cleanup:${selectedRun.id}:${Date.now()}` })}
          onFreeze={() => runAction('freeze', { reason: 'teacher_support', idempotency_key: `teacher-freeze:${selectedRun.id}:${Date.now()}` })}
          onCheck={() => runAction('check', { profile_id: 'lab-3-storage', idempotency_key: `teacher-check:${selectedRun.id}:${Date.now()}` })}
          finishing={busy}
        />
      ) : null}

      <LabEditor
        draft={draft}
        setDraft={setDraft}
        saveDefinition={saveDefinition}
        busy={busy}
        images={images}
        flavors={flavors}
        definitions={definitions}
      />
    </div>
  )
}

function ProjectPoolPanel({ pool, onImport, busy }) {
  const projects = pool.projects ?? []
  const states = pool.states ?? {}
  const warning = projectPoolWarning(projects, states)
  const visibleProjects = projects.slice(0, 8)
  const [seedJSON, setSeedJSON] = useState(projectSeedTemplate)
  const [error, setError] = useState('')

  async function importSeed() {
    try {
      const seed = parseProjectPoolSeed(seedJSON)
      setError('')
      await onImport(seed)
    } catch (nextError) {
      setError(nextError.message)
    }
  }

  return (
    <section className="panel project-pool-panel">
      <div className="panel-header">
        <div>
          <h2>Пул проектов КИ</h2>
          <p>{projects.length} projects</p>
        </div>
        <div className="pool-stats" aria-label="Project pool states">
          <span>
            <strong>{projectStateCount(states, 'FREE')}</strong> FREE
          </span>
          <span>
            <strong>{projectStateCount(states, 'ALLOCATED')}</strong> ALLOCATED
          </span>
          <span>
            <strong>{projectStateCount(states, 'QUARANTINED')}</strong> QUARANTINED
          </span>
        </div>
      </div>
      {warning ? <p className="form-error">{warning}</p> : null}
      <div className="pool-import">
        <label>
          <span>Seed JSON</span>
          <textarea value={seedJSON} onChange={(event) => setSeedJSON(event.target.value)} spellCheck="false" />
        </label>
        <button className="secondary-button" type="button" onClick={importSeed} disabled={busy}>
          {busy ? <Loader2 className="spin" size={18} /> : <Plus size={18} />}
          Импортировать
        </button>
      </div>
      {error ? <p className="form-error">{error}</p> : null}
      <div className="pool-list">
        {visibleProjects.map((project) => (
          <div className="pool-row" key={project.id}>
            <div>
              <strong>{project.name}</strong>
              <span>
                {project.course_id || 'course?'} / {project.domain_id}
              </span>
              <span>{project.id}</span>
            </div>
            <StateBadge state={project.state} />
            <div className="pool-owner">
              <span>{project.reserved_by_student_id || 'free'}</span>
              {project.current_lab_run_id ? <span>{shortID(project.current_lab_run_id)}</span> : null}
            </div>
          </div>
        ))}
        {projects.length === 0 ? <p className="empty">Пул проектов пуст</p> : null}
        {projects.length > visibleProjects.length ? <p className="empty">Показаны первые {visibleProjects.length} проектов</p> : null}
      </div>
    </section>
  )
}

function RuntimeSettingsPanel({ settings, onSave, busy }) {
  const [draft, setDraft] = useState(settings)
  const [error, setError] = useState('')

  function update(field, value) {
    setDraft((current) => ({ ...current, [field]: Number(value) || 0 }))
  }

  async function save() {
    const validationError = validateRuntimeSettings(draft)
    if (validationError) {
      setError(validationError)
      return
    }
    setError('')
    await onSave(draft)
  }

  return (
    <section className="panel settings-panel">
      <div className="panel-header">
        <h2>Параметры жизненного цикла</h2>
        <button className="primary-button" type="button" onClick={save} disabled={busy}>
          {busy ? <Loader2 className="spin" size={18} /> : <Save size={18} />}
          Сохранить
        </button>
      </div>
      {error ? <p className="form-error">{error}</p> : null}
      <div className="form-grid settings-grid">
        <label>
          <span>TTL стенда, мин</span>
          <input
            type="number"
            min="1"
            value={draft.lab_ttl_minutes}
            onChange={(event) => update('lab_ttl_minutes', event.target.value)}
          />
        </label>
        <label>
          <span>Заморозка, ч</span>
          <input
            type="number"
            min="1"
            value={draft.freeze_ttl_hours}
            onChange={(event) => update('freeze_ttl_hours', event.target.value)}
          />
        </label>
        <label>
          <span>Лимит кластера, %</span>
          <input
            type="number"
            min="1"
            max="100"
            value={draft.capacity_threshold_percent}
            onChange={(event) => update('capacity_threshold_percent', event.target.value)}
          />
        </label>
      </div>
    </section>
  )
}

function RunPanel({ run, instances, title, onFinish, onFreeze, onCheck, finishing, compact = false }) {
  const readyInstances = instances.filter((item) => item.state === 'ACTIVE').length
  const canFinish = run.state !== 'FINISHED' && run.state !== 'CLEANING'
  const canUseActiveAction = !terminalStates.has(run.state) && run.state !== 'CLEANING'

  return (
    <section className={compact ? 'panel run-panel compact-run' : 'panel run-panel'}>
      <div className="panel-header">
        <div>
          <h2>{title}</h2>
          <p>{run.id}</p>
        </div>
        <StateBadge state={run.state} />
      </div>
      {run.failure_message ? <Notice tone="danger">{run.failure_message}</Notice> : null}
      <div className="metrics-row inner">
        <Metric icon={Server} label="VM active" value={`${readyInstances}/${instances.length || 0}`} />
        <Metric icon={Activity} label="Таймер" value={run.cleanup_due_at ? timeLeft(run.cleanup_due_at) : 'нет'} />
      </div>
      <div className="instance-grid">
        {instances.map((instance) => (
          <div className="instance-row" key={instance.name}>
            <div>
              <strong>{instance.name}</strong>
              <span>{instance.fixed_ip || 'IP pending'}</span>
            </div>
            <StateBadge state={instance.state} />
            <button
              className="icon-button"
              type="button"
              disabled={!instance.vdi_access?.available}
              onClick={() => window.open(instance.vdi_access.url, '_blank', 'noopener,noreferrer')}
              title="VDI"
            >
              <DoorOpen size={18} />
            </button>
          </div>
        ))}
        {instances.length === 0 ? <p className="empty">VM появятся после разворачивания</p> : null}
      </div>
      <div className="action-row">
        {onCheck ? (
          <button className="secondary-button" type="button" onClick={onCheck} disabled={finishing || !canUseActiveAction}>
            <CheckCircle2 size={18} />
            Проверить
          </button>
        ) : null}
        {onFreeze ? (
          <button className="secondary-button" type="button" onClick={onFreeze} disabled={finishing || !canUseActiveAction}>
            <CircleAlert size={18} />
            Поддержка
          </button>
        ) : null}
        <button className="danger-button" type="button" onClick={onFinish} disabled={finishing || !canFinish}>
          {finishing ? <Loader2 className="spin" size={18} /> : <Trash2 size={18} />}
          Завершить
        </button>
      </div>
    </section>
  )
}

function LabEditor({ draft, setDraft, saveDefinition, busy, images, flavors, definitions }) {
  const courseOptions = useMemo(() => uniqueCourses(definitions, draft.course_id), [definitions, draft.course_id])
  const diskTotal = draft.instances.reduce((sum, instance) => sum + (Number(instance.disk_gib) || 0), 0)

  function update(field, value) {
    setDraft((current) => ({ ...current, [field]: value }))
  }

  function updateResource(field, value) {
    setDraft((current) => ({
      ...current,
      resources: { ...current.resources, [field]: Number(value) || 0 },
    }))
  }

  function updateInstance(index, field, value) {
    setDraft((current) => {
      const instances = current.instances.map((instance, itemIndex) => {
        if (itemIndex !== index) {
          return instance
        }
        const next = { ...instance, [field]: field === 'disk_gib' ? Number(value) || 0 : value }
        if (field === 'image_id') {
          next.disk_gib = Math.max(Number(next.disk_gib) || 0, imageMinDisk(value, images))
        }
        return next
      })
      if (field === 'image_id' || field === 'flavor_id' || field === 'disk_gib') {
        return { ...current, instances, resources: deriveResources(instances, images, flavors, current.resources) }
      }
      return { ...current, instances }
    })
  }

  function addInstance() {
    setDraft((current) => ({
      ...current,
      ...withDerivedResources({
        instances: [...current.instances, createDefaultInstance(current.instances.length + 1, images, flavors)],
        resources: current.resources,
      }, images, flavors),
    }))
  }

  function removeInstance(index) {
    setDraft((current) => {
      const instances = current.instances.filter((_, itemIndex) => itemIndex !== index)
      return { ...current, instances, resources: deriveResources(instances, images, flavors, current.resources) }
    })
  }

  return (
    <section className="panel editor-panel">
      <div className="panel-header">
        <h2>Конфигурация лабораторной</h2>
        <button className="primary-button" type="button" onClick={saveDefinition} disabled={busy}>
          {busy ? <Loader2 className="spin" size={18} /> : <Save size={18} />}
          Сохранить
        </button>
      </div>
      <div className="form-grid">
        <label>
          <span>Курс</span>
          <select value={draft.course_id} onChange={(event) => update('course_id', event.target.value)}>
            {courseOptions.map((courseID) => (
              <option key={courseID} value={courseID}>
                {courseID}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Код в LMS</span>
          <input value={draft.lab_id} readOnly />
        </label>
        <label className="wide">
          <span>Название</span>
          <input value={draft.title} onChange={(event) => update('title', event.target.value)} />
        </label>
        <label className="wide">
          <span>Описание</span>
          <textarea value={draft.description} onChange={(event) => update('description', event.target.value)} />
        </label>
        <label>
          <span>vCPU</span>
          <input type="number" min="1" value={draft.resources.vcpu} onChange={(event) => updateResource('vcpu', event.target.value)} />
        </label>
        <label>
          <span>RAM MiB</span>
          <input type="number" min="1" value={draft.resources.ram_mib} onChange={(event) => updateResource('ram_mib', event.target.value)} />
        </label>
        <label>
          <span>Disk GiB</span>
          <input type="number" min="1" value={draft.resources.disk_gib} onChange={(event) => updateResource('disk_gib', event.target.value)} />
        </label>
        <label className="toggle-row">
          <input type="checkbox" checked={draft.enabled} onChange={(event) => update('enabled', event.target.checked)} />
          <span>Доступна студентам</span>
        </label>
      </div>

      <div className="panel-header subheader">
        <h3>Виртуальные машины</h3>
        <span className={diskTotal > draft.resources.disk_gib ? 'inline-warning' : 'inline-hint'}>
          Диск VM: {diskTotal}/{draft.resources.disk_gib} GiB
        </span>
        <button className="secondary-button" type="button" onClick={addInstance}>
          <Plus size={18} />
          VM
        </button>
      </div>
      <div className="vm-editor">
        {draft.instances.map((instance, index) => (
          <div className="vm-row" key={`${instance.name}-${index}`}>
            <input value={instance.name} onChange={(event) => updateInstance(index, 'name', event.target.value)} aria-label="VM name" />
            <select value={instance.image_id} onChange={(event) => updateInstance(index, 'image_id', event.target.value)} aria-label="Image">
              <option value="">Image</option>
              {images.map((image) => (
                <option key={image.id} value={image.id}>
                  {image.name || image.id}
                </option>
              ))}
            </select>
            <select value={instance.flavor_id} onChange={(event) => updateInstance(index, 'flavor_id', event.target.value)} aria-label="Flavor">
              <option value="">Flavor</option>
              {flavors.map((flavor) => (
                <option key={flavor.id} value={flavor.id}>
                  {flavor.name || flavor.id}
                </option>
              ))}
            </select>
            <input value={instance.fixed_ip || ''} onChange={(event) => updateInstance(index, 'fixed_ip', event.target.value)} placeholder="10.0.0.10" />
            <input type="number" min="1" value={instance.disk_gib} onChange={(event) => updateInstance(index, 'disk_gib', event.target.value)} aria-label="Disk GiB" />
            <button className="icon-button" type="button" onClick={() => removeInstance(index)} aria-label="Удалить VM" title="Удалить VM">
              <Trash2 size={18} />
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}

function LabDefinitionSummary({ definition }) {
  return (
    <div className="definition-summary">
      <p>{definition.description}</p>
      <span>{definition.resources.vcpu} vCPU</span>
      <span>{Math.round(definition.resources.ram_mib / 1024)} GiB RAM</span>
      <span>{definition.resources.disk_gib} GiB Disk</span>
      <span>{definition.instances.length} VM</span>
    </div>
  )
}

function Metric({ icon: Icon, label, value }) {
  return (
    <div className="metric">
      <Icon size={18} />
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function StateBadge({ state }) {
  return <span className={`state state-${String(state).toLowerCase().replaceAll('_', '-')}`}>{state}</span>
}

function Notice({ children, tone = 'info' }) {
  return <div className={`notice ${tone}`}>{children}</div>
}

async function requestJSON(path, options = {}) {
  const response = await fetch(path, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options,
  })
  const text = await response.text()
  const data = text ? JSON.parse(text) : {}
  if (!response.ok) {
    throw new Error(data.error?.message || `HTTP ${response.status}`)
  }
  return data
}

async function loadStudentSnapshot() {
  const [catalog, labRuns] = await Promise.all([
    requestJSON('/api/lab-definitions'),
    requestJSON('/api/labs?limit=25'),
  ])
  return {
    definitions: catalog.labs ?? [],
    runs: labRuns.labs ?? [],
  }
}

async function loadTeacherSnapshot() {
  const [teacherCatalog, labRuns, settings, projectPool] = await Promise.all([
    requestJSON('/api/teacher/lab-definitions'),
    requestJSON('/api/labs?limit=100'),
    requestJSON('/api/admin/settings'),
    requestJSON('/api/admin/project-pool'),
  ])
  return {
    definitions: teacherCatalog.labs ?? [],
    runs: labRuns.labs ?? [],
    settings: normalizeRuntimeSettings(settings.values),
    projectPool: {
      states: projectPool.states ?? {},
      projects: projectPool.projects ?? [],
    },
  }
}

function labKey(lab) {
  return `${lab.course_id}:${lab.lab_id}`
}

function definitionTitle(run, definitions) {
  const definition = definitions.find((lab) => lab.course_id === run.course_id && lab.lab_id === run.lab_id)
  return definition?.title || run.lab_id
}

function cloneDefinition(definition) {
  return {
    ...definition,
    resources: { ...(definition.resources || emptyDefinition.resources) },
    instances: (definition.instances || []).map((instance) => ({ ...instance })),
  }
}

function normalizeDraft(draft) {
  const resources = draft.resources || emptyDefinition.resources
  return {
    course_id: String(draft.course_id || '').trim(),
    lab_id: String(draft.lab_id || '').trim(),
    title: String(draft.title || '').trim(),
    description: String(draft.description || '').trim(),
    enabled: Boolean(draft.enabled),
    resources: {
      vcpu: Number(resources.vcpu) || 0,
      ram_mib: Number(resources.ram_mib) || 0,
      disk_gib: Number(resources.disk_gib) || 0,
    },
    instances: (draft.instances || []).map((instance) => ({
      name: String(instance.name || '').trim(),
      image_id: String(instance.image_id || '').trim(),
      flavor_id: String(instance.flavor_id || '').trim(),
      fixed_ip: String(instance.fixed_ip || '').trim(),
      disk_gib: Number(instance.disk_gib) || 0,
    })),
  }
}

function validateDraft(draft) {
  if (!draft.course_id) {
    return 'Выберите курс'
  }
  if (!draft.lab_id) {
    return 'Не удалось сформировать код лабораторной'
  }
  if (!draft.title) {
    return 'Заполните название лабораторной'
  }
  if (draft.resources.vcpu <= 0 || draft.resources.ram_mib <= 0 || draft.resources.disk_gib <= 0) {
    return 'Ресурсы лабораторной должны быть больше нуля'
  }
  if (draft.instances.length === 0) {
    return 'Добавьте хотя бы одну виртуальную машину'
  }
  const names = new Set()
  let diskTotal = 0
  for (const [index, instance] of draft.instances.entries()) {
    const row = index + 1
    if (!instance.name) {
      return `Заполните имя VM ${row}`
    }
    if (names.has(instance.name)) {
      return `Имя VM "${instance.name}" повторяется`
    }
    names.add(instance.name)
    if (!instance.image_id) {
      return `Выберите образ для VM ${row}`
    }
    if (!instance.flavor_id) {
      return `Выберите flavor для VM ${row}`
    }
    if (instance.disk_gib <= 0) {
      return `Укажите размер диска для VM ${row}`
    }
    diskTotal += instance.disk_gib
  }
  if (diskTotal > draft.resources.disk_gib) {
    return `Суммарный диск VM (${diskTotal} GiB) больше лимита лабораторной (${draft.resources.disk_gib} GiB)`
  }
  return ''
}

function normalizeRuntimeSettings(values = {}) {
  return {
    lab_ttl_minutes: secondsToMinutes(values.lab_ttl_seconds, defaultRuntimeSettings.lab_ttl_minutes),
    freeze_ttl_hours: secondsToHours(values.freeze_ttl_seconds, defaultRuntimeSettings.freeze_ttl_hours),
    capacity_threshold_percent: Number(values.capacity_threshold_percent) || defaultRuntimeSettings.capacity_threshold_percent,
  }
}

function runtimeSettingsPayload(settings) {
  return {
    lab_ttl_seconds: Math.round(Number(settings.lab_ttl_minutes) * 60),
    freeze_ttl_seconds: Math.round(Number(settings.freeze_ttl_hours) * 3600),
    capacity_threshold_percent: Number(settings.capacity_threshold_percent),
  }
}

function validateRuntimeSettings(settings) {
  if (Number(settings.lab_ttl_minutes) <= 0) {
    return 'TTL стенда должен быть больше нуля'
  }
  if (Number(settings.freeze_ttl_hours) <= 0) {
    return 'Время заморозки должно быть больше нуля'
  }
  const threshold = Number(settings.capacity_threshold_percent)
  if (threshold <= 0 || threshold > 100) {
    return 'Лимит кластера должен быть в диапазоне 1-100%'
  }
  return ''
}

function projectPoolWarning(projects, states) {
  if (projects.length === 0) {
    return 'Пул проектов КИ пуст: LTI запуск не сможет выделить изолированный project'
  }
  if (projectStateCount(states, 'FREE') === 0) {
    return 'В пуле нет свободных проектов: новые LTI запуски будут отклонены'
  }
  if (projects.length === 1) {
    return 'В пуле только один project: параллельные студенческие запуски быстро исчерпают емкость'
  }
  return ''
}

function parseProjectPoolSeed(raw) {
  let seed
  try {
    seed = JSON.parse(raw)
  } catch {
    throw new Error('Seed должен быть валидным JSON')
  }
  if (!Array.isArray(seed.domains) || seed.domains.length === 0) {
    throw new Error('Seed должен содержать domains')
  }
  if (!Array.isArray(seed.projects) || seed.projects.length === 0) {
    throw new Error('Seed должен содержать projects')
  }
  return {
    domains: seed.domains,
    projects: seed.projects,
  }
}

function projectStateCount(states, state) {
  return Number(states?.[state] ?? 0)
}

function shortID(value) {
  if (!value || value.length <= 14) {
    return value
  }
  return `${value.slice(0, 8)}...${value.slice(-6)}`
}

function secondsToMinutes(value, fallback) {
  const seconds = Number(value)
  return seconds > 0 ? Math.round(seconds / 60) : fallback
}

function secondsToHours(value, fallback) {
  const seconds = Number(value)
  return seconds > 0 ? Math.round(seconds / 3600) : fallback
}

function createNewDefinitionDraft(definitions, images, flavors) {
  const courseID = definitions[0]?.course_id || emptyDefinition.course_id
  const number = nextLabNumber(definitions)
  const draft = {
    ...emptyDefinition,
    course_id: courseID,
    lab_id: uniqueLabID(`lab-${number}`, definitions, courseID),
    title: `Лабораторная ${number}`,
    description: '',
    instances: [createDefaultInstance(1, images, flavors)],
  }
  return withDerivedResources(draft, images, flavors)
}

function createDefaultInstance(index, images, flavors) {
  const image = defaultImage(images)
  const flavor = defaultFlavor(flavors)
  return {
    name: `VM-${index}`,
    image_id: image?.id || '',
    flavor_id: flavor?.id || '',
    fixed_ip: `10.0.0.${9 + index}`,
    disk_gib: image ? imageMinDisk(image.id, images) : emptyDefinition.instances[0].disk_gib,
  }
}

function withDerivedResources(draft, images, flavors) {
  return {
    ...draft,
    resources: deriveResources(draft.instances, images, flavors, draft.resources),
  }
}

function deriveResources(instances, images, flavors, fallback) {
  const totals = instances.reduce(
    (acc, instance) => {
      const flavor = flavors.find((item) => item.id === instance.flavor_id)
      acc.vcpu += Number(flavor?.vcpus) || 0
      acc.ram_mib += Number(flavor?.ram_mib) || 0
      acc.disk_gib += Math.max(Number(instance.disk_gib) || 0, imageMinDisk(instance.image_id, images))
      return acc
    },
    { vcpu: 0, ram_mib: 0, disk_gib: 0 },
  )
  return {
    vcpu: Math.max(totals.vcpu, Number(fallback?.vcpu) || 1),
    ram_mib: Math.max(totals.ram_mib, Number(fallback?.ram_mib) || 1024),
    disk_gib: Math.max(totals.disk_gib, Number(fallback?.disk_gib) || 1),
  }
}

function imageMinDisk(imageID, images) {
  const image = images.find((item) => item.id === imageID)
  return Number(image?.min_disk_gib) || Number(image?.size_gib) || 1
}

function defaultImage(images) {
  return (
    images.find((image) => image.status === 'active' && image.disk_format !== 'iso' && image.name?.toLowerCase().includes('debian')) ||
    images.find((image) => image.status === 'active' && image.disk_format !== 'iso') ||
    images[0]
  )
}

function defaultFlavor(flavors) {
  return flavors.find((flavor) => flavor.name === 'small') || flavors.find((flavor) => flavor.name === 'edu 2x2') || flavors[0]
}

function uniqueCourses(definitions, currentCourseID) {
  const values = new Set([currentCourseID || emptyDefinition.course_id, emptyDefinition.course_id])
  definitions.forEach((definition) => values.add(definition.course_id))
  return [...values].filter(Boolean)
}

function nextLabNumber(definitions) {
  const numbers = definitions.flatMap((definition) => {
    const values = []
    const labMatch = definition.lab_id.match(/^lab-(\d+)/)
    if (labMatch) {
      values.push(Number(labMatch[1]))
    }
    const titleMatch = definition.title.match(/(\d+)/)
    if (titleMatch) {
      values.push(Number(titleMatch[1]))
    }
    return values.filter(Number.isFinite)
  })
  return Math.max(0, ...numbers) + 1
}

function uniqueLabID(baseID, definitions, courseID) {
  const existing = new Set(definitions.filter((definition) => definition.course_id === courseID).map((definition) => definition.lab_id))
  let candidate = baseID
  let suffix = 2
  while (existing.has(candidate)) {
    candidate = `${baseID}-${suffix}`
    suffix += 1
  }
  return candidate
}

function timeLeft(value) {
  const diff = new Date(value).getTime() - Date.now()
  if (diff <= 0) {
    return 'сейчас'
  }
  const minutes = Math.ceil(diff / 60000)
  if (minutes < 60) {
    return `${minutes} мин`
  }
  return `${Math.floor(minutes / 60)} ч ${minutes % 60} мин`
}

function consumeLaunchNotice() {
  if (typeof window === 'undefined') {
    return ''
  }
  const params = new URLSearchParams(window.location.search)
  const status = params.get('launch_status')
  if (!status) {
    return ''
  }
  params.delete('launch_status')
  params.delete('lti_launch_id')
  params.delete('lab_run_id')
  const nextURL = `${window.location.pathname}${params.toString() ? `?${params.toString()}` : ''}${window.location.hash}`
  window.history.replaceState({}, '', nextURL)
  if (status === 'ACTIVE_LAB_EXISTS') {
    return 'У вас уже есть активная лабораторная работа. Новый стенд не запускался.'
  }
  if (status === 'ACCEPTED' || status === 'ALREADY_ACCEPTED') {
    return 'Запуск лабораторной работы принят.'
  }
  return ''
}

function getInitialLaunchNotice() {
  if (launchNoticeCache === undefined) {
    launchNoticeCache = consumeLaunchNotice()
  }
  return launchNoticeCache
}

export default App
