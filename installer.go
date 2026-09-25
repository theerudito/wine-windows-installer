package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

var (
	errEmptyInstaller   = errors.New("seleccioná un archivo .exe o .msi")
	errInvalidInstaller = errors.New("el archivo debe tener extensión .exe o .msi")
	errEmptyTarget      = errors.New("seleccioná el ejecutable destino")
	errInvalidTarget    = errors.New("el destino debe ser un archivo .exe")
	errInvalidName      = errors.New("el nombre debe tener entre 1 y 80 caracteres y no contener saltos de línea ni separadores")
	errEmptyDesktop     = errors.New("no se encontró la carpeta Escritorio del usuario")
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
type CommandStarter func(ctx context.Context, name string, args ...string) error

type WineStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}

type OperationResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

type DesktopRequest struct {
	InstallerPath string `json:"installerPath"`
	TargetPath    string `json:"targetPath"`
	Name          string `json:"name"`
	IconPath      string `json:"iconPath"`
}

func validateInstallerPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errEmptyInstaller
	}
	if containsControlCharacter(path) {
		return errors.New("la ruta del instalador contiene caracteres no permitidos")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".exe" && ext != ".msi" {
		return errInvalidInstaller
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("el archivo seleccionado no existe")
		}
		return fmt.Errorf("no se pudo acceder al archivo: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("el archivo seleccionado no es un archivo regular")
	}
	return nil
}

func validateTargetPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errEmptyTarget
	}
	if containsControlCharacter(path) {
		return errors.New("la ruta del ejecutable contiene caracteres no permitidos")
	}
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errInvalidTarget
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("el ejecutable seleccionado no existe")
		}
		return fmt.Errorf("no se pudo acceder al ejecutable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("el ejecutable seleccionado no es un archivo regular")
	}
	return nil
}

func validateIconPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if containsControlCharacter(path) {
		return errors.New("la ruta del icono contiene caracteres no permitidos")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".png" && ext != ".svg" && ext != ".ico" {
		return errors.New("el icono debe ser .png, .svg o .ico")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("no se pudo acceder al icono: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("el icono seleccionado no es un archivo regular")
	}
	return nil
}

func validateDesktopName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return errInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return errInvalidName
		}
	}
	return nil
}

func containsControlCharacter(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func installerCommand(path string) ([]string, error) {
	if err := validateInstallerPath(path); err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".msi") {
		return []string{"wine", "msiexec", "/i", path}, nil
	}
	return []string{"wine", path}, nil
}

func desktopExecArg(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"', '`', '$':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func desktopEntry(req DesktopRequest) (string, string, error) {
	if err := validateInstallerPath(req.InstallerPath); err != nil {
		return "", "", err
	}
	if err := validateTargetPath(req.TargetPath); err != nil {
		return "", "", err
	}
	if err := validateDesktopName(req.Name); err != nil {
		return "", "", err
	}
	if err := validateIconPath(req.IconPath); err != nil {
		return "", "", err
	}
	execLine := "wine " + desktopExecArg(req.TargetPath)
	icon := ""
	if req.IconPath != "" {
		icon = "\nIcon=" + req.IconPath
	}
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nVersion=1.0\nName=%s\nExec=%s%s\nTerminal=false\nCategories=Utility;\n", req.Name, execLine, icon)
	filename := safeDesktopFilename(req.Name)
	return filename, content, nil
}

func safeDesktopFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	filename := strings.Trim(b.String(), "-")
	if filename == "" {
		filename = "windows-installer"
	}
	return filename + ".desktop"
}

func writeDesktopEntry(home string, req DesktopRequest) (string, error) {
	filename, content, err := desktopEntry(req)
	if err != nil {
		return "", err
	}
	desktopDir := filepath.Join(home, "Desktop")
	if info, statErr := os.Stat(desktopDir); statErr != nil || !info.IsDir() {
		return "", errEmptyDesktop
	}
	path := filepath.Join(desktopDir, filename)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		return "", fmt.Errorf("no se pudo crear el acceso directo: %w", err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		return "", fmt.Errorf("no se pudieron establecer permisos del acceso directo: %w", err)
	}
	return path, nil
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func defaultCommandStarter(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Start()
}

func detectWine(ctx context.Context, run CommandRunner) WineStatus {
	if runtime.GOOS != "linux" {
		return WineStatus{Message: "WindowsInstaller está pensado para Ubuntu/Linux."}
	}
	if _, err := exec.LookPath("wine"); err != nil {
		return WineStatus{Message: "Wine no está instalado."}
	}
	output, err := run(ctx, "wine", "--version")
	if err != nil {
		return WineStatus{Installed: true, Message: "Wine está instalado, pero no se pudo leer su versión."}
	}
	return WineStatus{Installed: true, Version: strings.TrimSpace(string(output)), Message: "Wine está listo para usar."}
}
