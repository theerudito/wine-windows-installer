package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx   context.Context
	run   CommandRunner
	start CommandStarter
}

func NewApp() *App { return &App{run: defaultCommandRunner, start: defaultCommandStarter} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) commandContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) SelectInstaller() (string, error) {
	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title:   "Seleccionar instalador de Windows",
		Filters: []wailsRuntime.FileFilter{{DisplayName: "Instaladores Windows (*.exe;*.msi)", Pattern: "*.exe;*.msi"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	return path, validateInstallerPath(path)
}

func (a *App) SelectExecutable() (string, error) {
	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title:   "Seleccionar ejecutable instalado",
		Filters: []wailsRuntime.FileFilter{{DisplayName: "Ejecutables Windows (*.exe)", Pattern: "*.exe"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	return path, validateTargetPath(path)
}

func (a *App) SelectIcon() (string, error) {
	path, err := wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title:   "Seleccionar icono",
		Filters: []wailsRuntime.FileFilter{{DisplayName: "Imágenes (*.png;*.svg;*.ico)", Pattern: "*.png;*.svg;*.ico"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	return path, validateIconPath(path)
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

func (a *App) RunInstaller(path string) OperationResult {
	command, err := installerCommand(path)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	return a.startWineCommand(command, "instalador")
}

func (a *App) RunTarget(path string) OperationResult {
	if err := validateTargetPath(path); err != nil {
		return OperationResult{Message: err.Error()}
	}
	return a.startWineCommand([]string{"wine", path}, "ejecutable")
}

func (a *App) startWineCommand(command []string, label string) OperationResult {
	if err := a.start(a.commandContext(), command[0], command[1:]...); err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo iniciar el %s: %v", label, err)}
	}
	return OperationResult{Success: true, Message: fmt.Sprintf("El %s se inició con Wine.", label)}
}

func (a *App) CreateShortcut(req DesktopRequest) OperationResult {
	path, err := os.UserHomeDir()
	if err != nil {
		return OperationResult{Message: fmt.Sprintf("No se pudo determinar el usuario actual: %v", err)}
	}
	shortcut, err := writeDesktopEntry(path, req)
	if err != nil {
		return OperationResult{Message: err.Error()}
	}
	return OperationResult{Success: true, Message: "Acceso directo creado en el Escritorio.", Output: filepath.ToSlash(shortcut)}
}
