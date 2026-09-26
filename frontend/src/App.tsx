import { useEffect, useState } from 'react'
import { Install, SelectIcon, SelectInstaller, WineStatus } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

type Result = { success: boolean; message: string }
type Wine = { installed: boolean; version: string; message: string }
const initialWine: Wine = { installed: false, version: '', message: 'Comprobando Wine...' }

function App() {
  const [installer, setInstaller] = useState('')
  const [icon, setIcon] = useState('')
  const [name, setName] = useState('')
  const [wine, setWine] = useState(initialWine)
  const [notice, setNotice] = useState<Result | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => { void WineStatus().then(setWine).catch(() => setWine({ installed: false, version: '', message: 'No se pudo consultar Wine.' })) }, [])
  useEffect(() => EventsOn('installer-status', (status: { status: string; message: string }) => setNotice({ success: status.status === 'completed', message: status.message })), [])

  const chooseInstaller = async () => {
    try { const selected = await SelectInstaller(); if (selected) { setInstaller(selected); setNotice(null) } }
    catch (error) { setNotice({ success: false, message: String(error) }) }
  }
  const chooseIcon = async () => {
    try { const selected = await SelectIcon(); if (selected) setIcon(selected) }
    catch (error) { setNotice({ success: false, message: String(error) }) }
  }
  const install = async () => {
    if (!installer || !name.trim()) { setNotice({ success: false, message: 'Seleccioná un archivo y completá el nombre visible.' }); return }
    setBusy(true); setNotice(null)
    try { setNotice(await Install({ installerPath: installer, name, iconPath: icon })) }
    catch { setNotice({ success: false, message: 'La instalación no pudo iniciarse.' }) }
    finally { setBusy(false) }
  }

  const fileName = installer.split(/[\\/]/).pop() || ''
  return <main className="app-shell">
    <div className="app-frame">
      <section className="workspace-card">
        <span className="section-kicker">WindowsInstaller</span>
        <div className="card-heading"><div><h1>Instalá tu aplicación</h1><p>Elegí un archivo de Windows y configurá su acceso directo.</p></div></div>
        <label className="field file-field"><span>Archivo .exe o .msi</span><button onClick={() => void chooseInstaller()} className={`file-picker ${installer ? 'has-file' : ''}`}><strong>{fileName || 'Seleccionar archivo'}</strong><small>{installer ? 'Archivo seleccionado' : 'Abrir diálogo nativo'}</small></button></label>
        <div className="details-grid">
          <label className="field"><span>Nombre visible</span><input value={name} onChange={e => setName(e.target.value)} placeholder="Mi aplicación" maxLength={80} /></label>
          <label className="field"><span>Icono <em>Opcional</em></span><div className="input-action"><input readOnly value={icon} placeholder="Icono predeterminado" /><button onClick={() => void chooseIcon()}>Elegir</button></div></label>
        </div>
        <div className="launch-bar"><span>{fileName || 'Ningún archivo seleccionado'}</span><button onClick={() => void install()} disabled={busy || !wine.installed} className="primary-action">{busy ? 'Instalando…' : 'Instalar'}</button></div>
      </section>
      {!wine.installed && <div className="wine-alert"><strong>Wine no está disponible.</strong><span>{wine.message}{wine.version && ` · ${wine.version}`}</span></div>}
      {notice && <div className={`notice ${notice.success ? 'success' : 'error'}`} role="status">{notice.message}</div>}
    </div>
  </main>
}

export default App
