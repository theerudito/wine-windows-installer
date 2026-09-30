package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

var (
	errEmptyInstaller   = errors.New("seleccioná un archivo .exe o .msi")
	errInvalidInstaller = errors.New("el archivo debe tener extensión .exe o .msi")
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
type CommandProcess interface{ Wait() error }
type CommandStarterWithEnv func(ctx context.Context, env []string, name string, args ...string) (CommandProcess, error)

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
type InstallerStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type InstalledApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type InstallRequest struct {
	InstallerPath string `json:"installerPath"`
}

type appConfig struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Prefix string `json:"prefix"`
	Icon   string `json:"icon"`
	Type   string `json:"type"`
}

func containsControlCharacter(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
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
			return errors.New("el archivo seleccionado no existe")
		}
		return fmt.Errorf("no se pudo acceder al archivo: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("el archivo seleccionado no es un archivo regular")
	}
	return nil
}

func applicationName(path string) string {
	name := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if name == "" {
		return "Windows application"
	}
	return name
}

func winePrefixForHome(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("no se pudo determinar el directorio personal del usuario")
	}
	p := filepath.Join(home, "programas")
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar el prefijo de Wine: %w", err)
	}
	return p, nil
}
func winePrefix() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("no se pudo determinar el directorio personal del usuario: %w", err)
	}
	return winePrefixForHome(home)
}

var userHomeDir = os.UserHomeDir

func wineEnvironmentForPrefix(prefix string) []string { return []string{"WINEPREFIX=" + prefix} }
func wineEnvironment() ([]string, error) {
	p, err := winePrefix()
	if err != nil {
		return nil, err
	}
	return wineEnvironmentForPrefix(p), nil
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

func copyPortableExecutable(source, prefix, name string) (string, error) {
	if !strings.EqualFold(filepath.Ext(source), ".exe") {
		return "", errors.New("solo se pueden copiar archivos .exe como portable")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer el ejecutable portable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("el ejecutable portable no es un archivo regular")
	}
	filename := filepath.Base(source)
	if filename == "." || filename == ".." || filename == "" || filename != filepath.Clean(filename) {
		return "", errors.New("el nombre del ejecutable portable no es válido")
	}
	appDir := filepath.Join(prefix, safeAppDirectory(name))
	destination := filepath.Join(appDir, filename)
	rel, err := filepath.Rel(prefix, destination)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("la ruta del ejecutable portable no es segura")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("no se pudo copiar el ejecutable portable: %w", err)
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar la carpeta de la aplicación: %w", err)
	}
	if err := os.WriteFile(destination, data, 0755); err != nil {
		return "", fmt.Errorf("no se pudo guardar el ejecutable portable: %w", err)
	}
	return destination, nil
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
	result := strings.Trim(b.String(), "-")
	if result == "" {
		result = "windows-installer"
	}
	return result + ".desktop"
}
func safeAppDirectory(name string) string {
	return strings.TrimSuffix(safeDesktopFilename(name), ".desktop")
}
func desktopExecArg(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		if r == '\\' || r == '"' || r == '`' || r == '$' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func desktopEntryContent(name, target, prefix string) string {
	return fmt.Sprintf("[Desktop Entry]\nType=Application\nVersion=1.0\nName=%s\nExec=env %s wine %s\nIcon=application-x-executable\nTerminal=false\nCategories=Utility;\nStartupNotify=true\n", name, desktopExecArg("WINEPREFIX="+prefix), desktopExecArg(target))
}

func writeAppFiles(home string, req InstallRequest, target, prefix string) (string, error) {
	name := applicationName(req.InstallerPath)
	appDir := filepath.Join(prefix, safeAppDirectory(name))
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar la configuración: %w", err)
	}
	configType := "portable"
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".msi") {
		configType = "msi"
	}
	config := appConfig{Name: name, Target: target, Prefix: prefix, Icon: "application-x-executable", Type: configType}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), append(data, '\n'), 0644); err != nil {
		return "", fmt.Errorf("no se pudo guardar la configuración: %w", err)
	}
	content := desktopEntryContent(name, target, prefix)
	filename := safeDesktopFilename(name)
	applications := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(applications, 0755); err != nil {
		return "", err
	}
	shortcut := filepath.Join(applications, filename)
	if err := os.WriteFile(shortcut, []byte(content), 0755); err != nil {
		return "", fmt.Errorf("no se pudo crear el acceso directo: %w", err)
	}
	desktop, err := desktopDirectory(home)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(desktop, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar el Escritorio: %w", err)
	}
	if err := os.WriteFile(filepath.Join(desktop, filename), []byte(content), 0755); err != nil {
		return "", fmt.Errorf("no se pudo copiar el acceso directo al Escritorio: %w", err)
	}
	return shortcut, nil
}

func listInstalledApps(prefix string) ([]InstalledApp, error) {
	entries, err := os.ReadDir(prefix)
	if err != nil {
		if os.IsNotExist(err) {
			return []InstalledApp{}, nil
		}
		return nil, fmt.Errorf("no se pudieron leer las aplicaciones instaladas: %w", err)
	}
	apps := make([]InstalledApp, 0)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() != safeAppDirectory(entry.Name()) {
			continue
		}
		config, err := managedAppConfig(prefix, entry.Name())
		if err != nil {
			continue
		}
		appType := config.Type
		if appType == "" {
			appType = "portable"
			if strings.HasPrefix(filepath.Clean(config.Target), filepath.Join(prefix, "drive_c")) {
				appType = "msi"
			}
		}
		apps = append(apps, InstalledApp{ID: entry.Name(), Name: config.Name, Type: appType})
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return apps, nil
}

func managedAppConfig(prefix, id string) (appConfig, error) {
	appDir, err := managedAppDirectory(prefix, id)
	if err != nil {
		return appConfig{}, err
	}
	dirInfo, err := os.Lstat(appDir)
	if err != nil || !dirInfo.IsDir() {
		return appConfig{}, errors.New("la carpeta de la aplicación seleccionada no es válida")
	}
	configPath := filepath.Join(appDir, "config.json")
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() {
		return appConfig{}, errors.New("la aplicación seleccionada no tiene una configuración administrada")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return appConfig{}, errors.New("no se pudo leer la configuración de la aplicación")
	}
	var config appConfig
	if json.Unmarshal(data, &config) != nil || config.Prefix != prefix || config.Name == "" || safeAppDirectory(config.Name) != id || (config.Type != "" && config.Type != "portable" && config.Type != "msi") {
		return appConfig{}, errors.New("la configuración de la aplicación seleccionada no es válida")
	}
	return config, nil
}

func managedAppDirectory(prefix, id string) (string, error) {
	if id == "" || filepath.Base(id) != id || id != safeAppDirectory(id) {
		return "", errors.New("la aplicación seleccionada no es válida")
	}
	path := filepath.Join(prefix, id)
	rel, err := filepath.Rel(prefix, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("la ruta de la aplicación seleccionada no es segura")
	}
	return path, nil
}

func safeMSIInstallDirectory(prefix, target string) (string, error) {
	if strings.TrimSpace(target) == "" || !filepath.IsAbs(target) || containsControlCharacter(target) {
		return "", errors.New("la ruta de instalación MSI no es segura")
	}
	driveRoot := filepath.Join(prefix, "drive_c")
	rel, err := filepath.Rel(driveRoot, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("la ruta de instalación MSI está fuera del prefijo administrado")
	}
	if !strings.EqualFold(filepath.Ext(target), ".exe") {
		return "", errors.New("la ruta de instalación MSI no identifica un ejecutable")
	}
	installDir := filepath.Dir(target)
	dirRel, err := filepath.Rel(driveRoot, installDir)
	if err != nil || dirRel == "." || dirRel == ".." || strings.HasPrefix(dirRel, ".."+string(filepath.Separator)) {
		return "", errors.New("la carpeta de instalación MSI no es específica de la aplicación")
	}
	parts := strings.Split(filepath.Clean(dirRel), string(filepath.Separator))
	if len(parts) < 2 {
		return "", errors.New("la carpeta de instalación MSI no es específica de la aplicación")
	}
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "windows", "system32", "syswow64", "program files", "program files (x86)", "common files":
			if part == parts[len(parts)-1] {
				return "", errors.New("la carpeta de instalación MSI no es específica de la aplicación")
			}
		}
	}
	if err := validateNoSymlinkPath(driveRoot, installDir); err != nil {
		return "", err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("el ejecutable MSI administrado no es un archivo regular")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("no se pudo validar el ejecutable MSI administrado: %w", err)
	}
	return installDir, nil
}

func validateNoSymlinkPath(root, path string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return errors.New("la raíz de la aplicación no es válida")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return errors.New("la ruta de la aplicación no es válida")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("la ruta de la aplicación no es segura")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("no se pudo validar la ruta de la aplicación: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("la ruta de la aplicación contiene un enlace simbólico")
		}
	}
	return nil
}

func removeManagedApp(home, prefix, id string) error {
	appDir, err := managedAppDirectory(prefix, id)
	if err != nil {
		return err
	}
	config, err := managedAppConfig(prefix, id)
	if err != nil {
		return err
	}
	appType := config.Type
	if appType == "" && strings.HasPrefix(filepath.Clean(config.Target), filepath.Join(prefix, "drive_c")) {
		appType = "msi"
	}
	var installDir string
	if appType == "msi" {
		installDir, err = safeMSIInstallDirectory(prefix, config.Target)
		if err != nil {
			return err
		}
	} else {
		if filepath.Dir(config.Target) != appDir || !strings.EqualFold(filepath.Ext(config.Target), ".exe") {
			return errors.New("el ejecutable portable no pertenece a la aplicación seleccionada")
		}
		info, err := os.Lstat(config.Target)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("el ejecutable portable no es un archivo regular administrado")
		}
	}
	if appType == "msi" {
		if err := os.RemoveAll(installDir); err != nil {
			return fmt.Errorf("no se pudo quitar la carpeta de instalación MSI: %w", err)
		}
	} else if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("no se pudo quitar la carpeta de la aplicación: %w", err)
	}
	if appType == "msi" {
		if err := os.RemoveAll(appDir); err != nil {
			return fmt.Errorf("no se pudo quitar la configuración: %w", err)
		}
	}
	shortcut := safeDesktopFilename(config.Name)
	for _, dir := range []string{filepath.Join(home, ".local", "share", "applications"), filepath.Join(home, "Desktop")} {
		path := filepath.Join(dir, shortcut)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("no se pudo comprobar el acceso directo: %w", err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("no se pudo leer el acceso directo: %w", err)
		}
		if string(content) != desktopEntryContent(config.Name, config.Target, prefix) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("no se pudo quitar el acceso directo: %w", err)
		}
	}
	return nil
}

func desktopDirectory(home string) (string, error) {
	return filepath.Join(home, "Desktop"), nil
}

func discoverInstalledExecutable(prefix string, before map[string]struct{}) (string, int) {
	root := filepath.Join(prefix, "drive_c")
	var candidates []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			lower := strings.ToLower(filepath.Base(path))
			if path != root && (lower == "windows" || lower == "system32" || lower == "syswow64" || lower == "$recycle.bin" || lower == "wine") {
				return filepath.SkipDir
			}
			return nil
		}
		lowerPath := strings.ToLower(path)
		if !strings.EqualFold(filepath.Ext(path), ".exe") || strings.EqualFold(filepath.Base(path), "uninstall.exe") || strings.Contains(lowerPath, "uninstall") || strings.Contains(lowerPath, "\\windows\\") || strings.Contains(lowerPath, "/windows/") || strings.Contains(lowerPath, "\\wine\\") || strings.Contains(lowerPath, "/wine/") {
			return nil
		}
		if _, existed := before[path]; existed {
			return nil
		}
		candidates = append(candidates, path)
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		score := func(p string) int {
			l := strings.ToLower(p)
			if strings.Contains(l, "program files") {
				return 0
			}
			if strings.Contains(l, "programfiles") {
				return 0
			}
			if strings.Contains(l, "program files (x86)") {
				return 1
			}
			return 2
		}
		si, sj := score(candidates[i]), score(candidates[j])
		if si != sj {
			return si < sj
		}
		return candidates[i] < candidates[j]
	})
	if len(candidates) == 1 {
		return candidates[0], 1
	}
	return "", len(candidates)
}

func snapshotExecutables(prefix string) map[string]struct{} {
	result := map[string]struct{}{}
	_ = filepath.WalkDir(filepath.Join(prefix, "drive_c"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".exe") {
			result[path] = struct{}{}
		}
		return nil
	})
	return result
}
func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
func defaultCommandStarterWithEnv(ctx context.Context, env []string, name string, args ...string) (CommandProcess, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), env...)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
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
