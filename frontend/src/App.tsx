import { useEffect, useState } from 'react'
import { Install, SelectInstaller, Uninstall, WineStatus } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

type Result = { success: boolean; message: string }
type Wine = { installed: boolean; version: string; message: string }
const initialWine: Wine = { installed: false, version: '', message: 'Comprobando Wine...' }

function App() {
  const [installer, setInstaller] = useState('')
  const [tab, setTab] = useState<'install' | 'uninstall'>('install')
  const [wine, setWine] = useState(initialWine)
  const [notice, setNotice] = useState<Result | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => { void WineStatus().then(setWine).catch(() => setWine({ installed: false, version: '', message: 'No se pudo consultar Wine.' })) }, [])
  useEffect(() => EventsOn('installer-status', (status: { status: string; message: string }) => setNotice({ success: status.status === 'completed', message: status.message })), [])

  const chooseInstaller = async () => {
    try { const selected = await SelectInstaller(); if (selected) { setInstaller(selected); setNotice(null) } }
    catch (error) { setNotice({ success: false, message: String(error) }) }
  }
  const install = async () => {
    if (!installer) { setNotice({ success: false, message: 'Seleccioná un archivo .exe o .msi.' }); return }
    setBusy(true); setNotice(null)
    try { setNotice(await Install({ installerPath: installer })) }
    catch { setNotice({ success: false, message: 'La instalación no pudo iniciarse.' }) }
    finally { setBusy(false) }
  }
  const uninstall = async () => {
    setBusy(true); setNotice(null)
    try { setNotice(await Uninstall()) }
    catch { setNotice({ success: false, message: 'El desinstalador no pudo iniciarse.' }) }
    finally { setBusy(false) }
  }

  const fileName = installer.split(/[\\/]/).pop() || ''
  return <main className="app-shell">
    <div className="app-frame">
      <section className="workspace-card">
        <span className="section-kicker">WindowsInstaller</span>
        <nav className="tabs" aria-label="Operaciones">
          <button className={tab === 'install' ? 'active' : ''} onClick={() => setTab('install')}>Instalar</button>
          <button className={tab === 'uninstall' ? 'active' : ''} onClick={() => setTab('uninstall')}>Desinstalar</button>
        </nav>
        {tab === 'install' ? <>
          <div className="card-heading"><div><h1>Instalá tu aplicación</h1><p>Elegí un archivo de Windows. El nombre y el icono se derivan automáticamente.</p></div></div>
          <label className="field file-field"><span>Archivo .exe o .msi</span><button onClick={() => void chooseInstaller()} className={`file-picker ${installer ? 'has-file' : ''}`}><strong>{fileName || 'Seleccionar archivo'}</strong><small>{installer ? 'Archivo seleccionado' : 'Abrir diálogo nativo'}</small></button></label>
          <div className="launch-bar"><span>{fileName || 'Ningún archivo seleccionado'}</span><button onClick={() => void install()} disabled={busy || !wine.installed} className="primary-action">Instalar</button></div>
        </> : <>
          <div className="card-heading"><div><h1>Desinstalá una aplicación</h1><p>Wine abrirá su desinstalador oficial. Allí elegí el programa registrado y ejecutá su desinstalador oficial.</p></div></div>
          <button onClick={() => void uninstall()} disabled={busy || !wine.installed} className="primary-action full-action">Desinstalar</button>
        </>}
      </section>
      {!wine.installed && <div className="wine-alert"><strong>Wine no está disponible.</strong><span>{wine.message}{wine.version && ` · ${wine.version}`}</span></div>}
      {notice && <div className={`notice ${notice.success ? 'success' : 'error'}`} role="status">{notice.message}</div>}
    </div>
  </main>
}

export default App
