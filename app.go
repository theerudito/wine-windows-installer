package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx      context.Context
	run      CommandRunner
	startEnv CommandStarterWithEnv
	emit     EventEmitter
}

type EventEmitter func(context.Context, string, ...interface{})

func NewApp() *App {
	return &App{run: defaultCommandRunner, startEnv: defaultCommandStarterWithEnv, emit: wailsRuntime.EventsEmit}
}
func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) commandContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) SelectInstaller() (string, error) {
	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{Title: "Seleccionar archivo de Windows", Filters: []wailsRuntime.FileFilter{{DisplayName: "Archivos Windows (*.exe;*.msi)", Pattern: "*.exe;*.msi"}}})
	if err != nil || path == "" {
		return path, err
	}
	return path, validateInstallerPath(path)
}

func (a *App) WineStatus() WineStatus { return detectWine(a.commandContext(), a.run) }
func (a *App) InstallWine() OperationResult {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return OperationResult{Message: "No se encontró pkexec. Instalá polkit para usar la autenticación del sistema."}
	}
	output, err := a.run(a.commandContext(), "pkexec", "apt-get", "install", "-y", "wine")
	result := OperationResult{Output: string(output)}
	if err != nil {
		result.Message = fmt.Sprintf("No se pudo instalar Wine: %v", err)
		return result
	}
	result.Success = true
	result.Message = "Wine se instaló correctamente."
	return result
}

// Install runs an MSI and records managed metadata after it completes.
func (a *App) Install(req InstallRequest) OperationResult {
	if err := validateInstallerPath(req.InstallerPath); err != nil {
		return OperationResult{Message: err.Error()}
	}
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".exe") {
		return a.RunPortable(req.InstallerPath)
	}
	prefix, err := winePrefix()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	target := req.InstallerPath
	command, err := installerCommand(target)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	env := wineEnvironmentForPrefix(prefix)
	before := snapshotExecutables(prefix)
	process, err := a.startEnv(a.commandContext(), env, command[0], command[1:]...)
	if err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo iniciar el archivo: %v", err)}
	}
	go a.finishInstall(process, req, target, prefix, before)
	a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "running", Message: "El archivo se está ejecutando con Wine."})
	return OperationResult{Success: true, Message: "La operación está en ejecución. Esperá a que finalice para registrar la instalación."}
}

func (a *App) RunPortable(path string) OperationResult {
	if err := validateInstallerPath(path); err != nil || !strings.EqualFold(filepath.Ext(path), ".exe") {
		if err == nil {
			err = errInvalidInstaller
		}
		return OperationResult{Message: err.Error()}
	}
	prefix, err := winePrefix()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	home, err := userHomeDir()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	target, err := copyPortableExecutable(path, prefix, applicationName(path))
	if err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo copiar la aplicación portable: %v", err)}
	}
	request := InstallRequest{InstallerPath: path}
	appDir, err := writeAppFiles(home, request, target, prefix)
	if err != nil {
		_ = removePortableDirectory(prefix, applicationName(path))
		return OperationResult{Message: fmt.Sprintf("No se pudo registrar la aplicación portable: %v", err)}
	}
	process, err := a.startEnv(a.commandContext(), wineEnvironmentForPrefix(prefix), "wine", target)
	if err != nil {
		_ = removeManagedApp(home, prefix, filepath.Base(appDir))
		return OperationResult{Message: fmt.Sprintf("No se pudo iniciar la aplicación: %v", err)}
	}
	go func() {
		if err := process.Wait(); err != nil {
			a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("La aplicación terminó con un error: %v.", err)})
			return
		}
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: "La aplicación finalizó correctamente."})
	}()
	a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "running", Message: "La aplicación se está ejecutando con Wine."})
	return OperationResult{Success: true, Message: "La aplicación se está ejecutando."}
}

func removePortableDirectory(prefix, name string) error {
	appDir, err := managedAppDirectory(prefix, safeAppDirectory(name))
	if err != nil {
		return err
	}
	return os.RemoveAll(appDir)
}

func (a *App) ListInstalledApps() ([]InstalledApp, error) {
	prefix, err := winePrefix()
	if err != nil {
		return nil, err
	}
	return listInstalledApps(prefix)
}

// Uninstall removes the selected managed application and opens Wine's uninstaller for MSI entries.
func (a *App) Uninstall(id string) OperationResult {
	prefix, err := winePrefix()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	home, err := userHomeDir()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	apps, err := listInstalledApps(prefix)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	var selected InstalledApp
	for _, app := range apps {
		if app.ID == id {
			selected = app
			break
		}
	}
	if selected.ID == "" {
		return OperationResult{Message: "Seleccioná una aplicación instalada."}
	}
	if selected.Type != "msi" {
		if err := removeManagedApp(home, prefix, id); err != nil {
			return OperationResult{Message: err.Error()}
		}
		return OperationResult{Success: true, Message: "La aplicación se desinstaló correctamente."}
	}
	config, err := managedAppConfig(prefix, id)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	if _, err := safeMSIInstallDirectory(prefix, config.Target, config.InstallDir); err != nil {
		return OperationResult{Message: err.Error()}
	}
	process, err := a.startEnv(a.commandContext(), wineEnvironmentForPrefix(prefix), "wine", "uninstaller")
	if err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo abrir el desinstalador de Wine: %v", err)}
	}
	go func() {
		if err := process.Wait(); err != nil {
			a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("El desinstalador terminó con un error: %v.", err)})
			return
		}
		if err := removeManagedApp(home, prefix, id); err != nil {
			a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("El desinstalador terminó, pero no se pudo completar la limpieza: %v.", err)})
			return
		}
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: "La aplicación se desinstaló correctamente y se quitaron sus archivos."})
	}()
	return OperationResult{Success: true, Message: "Se abrió el desinstalador de Wine. Seleccioná allí la aplicación que quieras quitar."}
}

func (a *App) finishInstall(process CommandProcess, req InstallRequest, target, prefix string, before map[string]struct{}) {
	if err := process.Wait(); err != nil {
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("La operación terminó con un error: %v.", err)})
		return
	}
	message := "La aplicación finalizó correctamente."
	installDir := ""
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".msi") {
		target, _, installDir = discoverInstalledLocation(prefix, before)
		message = "La instalación terminó correctamente."
	}
	home, err := userHomeDir()
	if err != nil {
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: message + " No se pudo registrar la instalación."})
		return
	}
	if _, err := writeAppFiles(home, req, target, prefix, installDir); err != nil {
		message += " No se pudo registrar la instalación."
	}
	a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: message})
}
