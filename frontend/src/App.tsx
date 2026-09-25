import { useEffect, useState } from 'react'
import { CreateShortcut, InstallWine, RunInstaller, RunTarget, SelectExecutable, SelectIcon, SelectInstaller, WineStatus } from '../wailsjs/go/main/App'

type Result = { success: boolean; message: string; output?: string }
type Wine = { installed: boolean; version: string; message: string }
type Mode = '' | 'portable' | 'installer'
type IconName = 'arrow' | 'check' | 'file' | 'folder' | 'launch' | 'spark' | 'wine'

const initialWine: Wine = { installed: false, version: '', message: 'Comprobando Wine...' }

function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  const common = { fill: 'none', stroke: 'currentColor', strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, strokeWidth: 1.8 }
  const paths = {
    arrow: <><path {...common} d="M12 5v14M7 10l5-5 5 5" /><path {...common} d="M5 19h14" /></>,
    check: <path {...common} d="m5 12 4.2 4.2L19 6.5" />,
    file: <><path {...common} d="M6.5 3.5h7l4 4V20a.5.5 0 0 1-.5.5h-10a.5.5 0 0 1-.5-.5V4a.5.5 0 0 1 .5-.5Z" /><path {...common} d="M13.5 3.5V8h4M9 12h6M9 15.5h4" /></>,
    folder: <><path {...common} d="M3.5 6.5h6l1.6 2h9.4v9.8a1.2 1.2 0 0 1-1.2 1.2H4.7a1.2 1.2 0 0 1-1.2-1.2V6.5Z" /><path {...common} d="M3.5 8.5h17" /></>,
    launch: <><path {...common} d="M14 4h6v6M20 4l-9 9" /><path {...common} d="M18 13v5.5a1.5 1.5 0 0 1-1.5 1.5h-12A1.5 1.5 0 0 1 3 18.5v-12A1.5 1.5 0 0 1 4.5 5H10" /></>,
    spark: <><path {...common} d="m12 3 1.3 5.7L19 10l-5.7 1.3L12 17l-1.3-5.7L5 10l5.7-1.3L12 3Z" /><path {...common} d="m19 15 .5 2.5L22 18l-2.5.5L19 21l-.5-2.5L16 18l2.5-.5L19 15Z" /></>,
    wine: <><path {...common} d="M7 3.5h10M8 3.5v4.1c0 2.2 1.5 3.6 4 4.5v4.4M16 3.5v4.1c0 2.2-1.5 3.6-4 4.5M8 20.5h8M12 16.5v4" /><path {...common} d="M8 7.5h8" /></>,
  }
  return <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24">{paths[name]}</svg>
}

function App() {
  const [installer, setInstaller] = useState('')
  const [target, setTarget] = useState('')
  const [mode, setMode] = useState<Mode>('')
  const [icon, setIcon] = useState('')
  const [name, setName] = useState('')
  const [wine, setWine] = useState(initialWine)
  const [wineChecked, setWineChecked] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<Result | null>(null)

  const refreshWine = async () => {
    try { setWine(await WineStatus()) } catch { setWine({ installed: false, version: '', message: 'No se pudo consultar Wine.' }) }
    finally { setWineChecked(true) }
  }
  useEffect(() => { void refreshWine() }, [])

  const run = async (operation: () => Promise<Result>) => {
    setBusy(true); setNotice(null)
    try { setNotice(await operation()) } catch { setNotice({ success: false, message: 'La operación no pudo completarse.' }) }
    finally { setBusy(false) }
  }

  const selectInstaller = async () => {
    try {
      const selected = await SelectInstaller()
      if (selected) {
        setInstaller(selected)
        setTarget('')
        setMode(selected.toLowerCase().endsWith('.msi') ? 'installer' : '')
      }
    } catch (error) { setNotice({ success: false, message: String(error) }) }
  }
  const selectExecutable = async () => {
    try { const selected = await SelectExecutable(); if (selected) setTarget(selected) }
    catch (error) { setNotice({ success: false, message: String(error) }) }
  }
  const selectIcon = async () => {
    try { const selected = await SelectIcon(); if (selected) setIcon(selected) }
    catch (error) { setNotice({ success: false, message: String(error) }) }
  }

  const runWithShortcut = async (targetPath: string, label: string): Promise<Result> => {
    if (!name.trim()) return { success: false, message: 'Ingresá un nombre visible antes de ejecutar.' }
    if (!targetPath.trim()) return { success: false, message: `Seleccioná el ejecutable ${label} antes de ejecutar.` }
    const shortcut = await CreateShortcut({ installerPath: installer, targetPath, name, iconPath: icon })
    if (!shortcut.success) return shortcut
    return RunTarget(targetPath)
  }

  const execute = async (): Promise<Result> => {
    if (mode === 'portable') return runWithShortcut(installer, 'portable')
    if (mode === 'installer' && !target.trim()) return RunInstaller(installer)
    return runWithShortcut(target, 'instalado')
  }

  const fileName = installer.split(/[\\/]/).pop() || ''
  const extension = fileName.split('.').pop()?.toUpperCase() || 'FILE'
  const isMsi = installer.toLowerCase().endsWith('.msi')
  const requiresName = mode === 'portable' || (mode === 'installer' && Boolean(target.trim()))

  return <main className="app-shell">
    <div className="ambient-glow ambient-glow-one" />
    <div className="ambient-glow ambient-glow-two" />
    <div className="app-frame">
      <section className="workspace-card">
        <div className="card-heading"><div><span className="section-kicker">Configuración</span><h2>Prepará tu aplicación</h2></div><span className="step-count">{installer ? '02' : '01'} <span>/ 03</span></span></div>

        <div className="step-section">
          <div className="step-label"><span className="step-number">01</span><div><h3>Elegí un archivo</h3><p>Compatible con instaladores `.exe` y `.msi`.</p></div></div>
          <button onClick={selectInstaller} className={`file-picker ${installer ? 'has-file' : ''}`}>
            <span className="file-icon"><Icon name={installer ? 'file' : 'arrow'} size={22} /></span>
            <span className="file-picker-copy"><strong>{installer ? fileName : 'Seleccionar archivo'}</strong><small>{installer ? 'Archivo seleccionado' : 'Abrir diálogo nativo'}</small></span>
            {installer && <span className="extension-tag">.{extension}</span>}
            {!installer && <Icon name="folder" size={19} />}
          </button>
        </div>

        {installer && <>
          <div className="divider" />
          <div className="step-section compact-section">
            <div className="step-label"><span className="step-number">02</span><div><h3>Definí cómo usarla</h3><p>{isMsi ? 'Los archivos MSI se ejecutan como instaladores.' : 'Elegí si el archivo ya está listo o requiere instalación.'}</p></div></div>
            <fieldset className="mode-grid">
              <legend className="sr-only">Modo de uso</legend>
              <label className={`mode-card ${mode === 'portable' ? 'selected' : ''} ${isMsi ? 'disabled' : ''}`}><input type="radio" name="mode" value="portable" checked={mode === 'portable'} disabled={isMsi} onChange={() => { setMode('portable'); setTarget(installer) }} /><span className="mode-icon"><Icon name="launch" size={19} /></span><span><strong>Aplicación portable</strong><small>Ejecutar este archivo directamente</small></span><span className="radio-indicator"><Icon name="check" size={13} /></span></label>
              <label className={`mode-card ${mode === 'installer' ? 'selected' : ''}`}><input type="radio" name="mode" value="installer" checked={mode === 'installer'} onChange={() => { setMode('installer'); setTarget('') }} /><span className="mode-icon"><Icon name="file" size={19} /></span><span><strong>Instalador</strong><small>Instalar y elegir el ejecutable final</small></span><span className="radio-indicator"><Icon name="check" size={13} /></span></label>
            </fieldset>
          </div>

          <div className="divider" />
          <div className="details-grid">
            <label className="field"><span>Nombre visible</span><input value={name} onChange={e => setName(e.target.value)} placeholder="Mi aplicación" maxLength={80} /></label>
            <label className="field"><span>Icono <em>Opcional</em></span><div className="input-action"><input readOnly value={icon} placeholder="Ningún icono seleccionado" /><button onClick={selectIcon} aria-label="Elegir icono">Elegir</button></div></label>
          </div>

          {mode === 'installer' && <div className="target-panel"><div className="target-heading"><span className="target-icon"><Icon name="folder" size={18} /></span><div><strong>Ejecutable instalado</strong><p>Elegí el archivo que querés abrir después de instalar.</p></div></div><div className="input-action"><input readOnly value={target} placeholder="Ningún ejecutable seleccionado" /><button onClick={selectExecutable}>Elegir archivo</button></div></div>}

          <div className="divider" />
          <div className="launch-section"><div className="launch-heading"><span className="step-number">03</span><div><h3>{mode === 'installer' && !target ? 'Listo para instalar' : 'Listo para ejecutar'}</h3><p>{mode === 'installer' && !target ? 'Instalá la aplicación para elegir el ejecutable final.' : 'El acceso directo se crea automáticamente antes de abrir la aplicación.'}</p></div></div><div className="launch-bar"><div className="launch-file"><span className="launch-file-icon"><Icon name="file" size={17} /></span><span>{mode === 'installer' && target ? target.split(/[\\/]/).pop() : fileName}</span></div>{(mode === 'portable' || mode === 'installer') && <button onClick={() => void run(execute)} disabled={busy || !wine.installed || (requiresName && !name.trim())} className="primary-action"><Icon name="launch" size={17} />{mode === 'portable' ? 'Ejecutar aplicación' : target ? 'Ejecutar aplicación instalada' : 'Instalar aplicación'}</button>}</div></div>
        </>}
      </section>

      {wineChecked && !wine.installed && <div className="wine-alert"><span className="wine-alert-icon"><Icon name="wine" size={20} /></span><div><strong>Wine no está instalado</strong><p>{wine.message}{wine.version && ` · ${wine.version}`}</p></div><button onClick={() => void run(async () => { const result = await InstallWine(); if (result.success) await refreshWine(); return result })} disabled={busy}>Instalar Wine</button></div>}
      {notice && <div className={`notice ${notice.success ? 'success' : 'error'}`} role="status"><span className="notice-icon"><Icon name={notice.success ? 'check' : 'spark'} size={17} /></span><p><strong>{notice.success ? 'Listo' : 'Error'}</strong>{notice.message}</p></div>}
    </div>
  </main>
}

export default App
