package main

import (
	"context"
	"fmt"
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

// Install is the only application operation: it runs the selected EXE/MSI and creates its configuration and shortcuts.
func (a *App) Install(req InstallRequest) OperationResult {
	if err := validateInstallerPath(req.InstallerPath); err != nil {
		return OperationResult{Message: err.Error()}
	}
	name := applicationName(req.InstallerPath)
	prefix, err := winePrefix()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	target := req.InstallerPath
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".exe") {
		target, err = copyPortableExecutable(req.InstallerPath, prefix, name)
		if err != nil {
			return OperationResult{Message: err.Error()}
		}
	}
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
	return OperationResult{Success: true, Message: "La operación está en ejecución. Esperá a que finalice para crear el acceso directo."}
}

// Uninstall opens Wine's official uninstaller for the shared application prefix.
func (a *App) Uninstall() OperationResult {
	prefix, err := winePrefix()
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	process, err := a.startEnv(a.commandContext(), wineEnvironmentForPrefix(prefix), "wine", "uninstaller")
	if err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo abrir el desinstalador de Wine: %v", err)}
	}
	go func() {
		if err := process.Wait(); err != nil {
			a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("El desinstalador terminó con un error: %v.", err)})
		}
	}()
	return OperationResult{Success: true, Message: "Se abrió el desinstalador oficial de Wine."}
}

func (a *App) finishInstall(process CommandProcess, req InstallRequest, target, prefix string, before map[string]struct{}) {
	if err := process.Wait(); err != nil {
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "failed", Message: fmt.Sprintf("La operación terminó con un error: %v.", err)})
		return
	}
	message := "La aplicación finalizó correctamente."
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".msi") {
		var count int
		target, count = discoverInstalledExecutable(prefix, before)
		if count != 1 {
			a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: "La instalación terminó, pero no se pudo crear automáticamente el acceso directo porque no se encontró un único ejecutable instalado."})
			return
		}
		message = "La instalación terminó correctamente."
	}
	home, err := userHomeDir()
	if err != nil {
		a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: message + " No se pudo crear automáticamente el acceso directo."})
		return
	}
	if _, err := writeAppFiles(home, req, target, prefix); err != nil {
		message += " No se pudo crear automáticamente el acceso directo."
	} else {
		message += " Se creó el acceso directo."
	}
	a.emit(a.commandContext(), "installer-status", InstallerStatus{Status: "completed", Message: message})
}
