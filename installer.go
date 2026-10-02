package main

import (
	"bytes"
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
	"sync"
	"unicode"
)

var (
	errEmptyInstaller   = errors.New("seleccioná un archivo .exe o .msi")
	errInvalidInstaller = errors.New("el archivo debe tener extensión .exe o .msi")
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
type CommandProcess interface {
	Wait() error
	Output() string
}
type CommandStarterWithEnv func(ctx context.Context, env []string, workingDir, name string, args ...string) (CommandProcess, error)

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
	Name       string `json:"name"`
	Target     string `json:"target"`
	InstallDir string `json:"installDir,omitempty"`
	Prefix     string `json:"prefix"`
	Icon       string `json:"icon"`
	Type       string `json:"type"`
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
	info, err := os.Lstat(path)
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
	p := filepath.Join(home, "Programs Wine")
	legacy := filepath.Join(home, "programas")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			if err := os.Rename(legacy, p); err != nil {
				return "", fmt.Errorf("no se pudo migrar el prefijo anterior de Wine: %w", err)
			}
		} else if !os.IsNotExist(legacyErr) {
			return "", fmt.Errorf("no se pudo comprobar el prefijo anterior de Wine: %w", legacyErr)
		}
	} else if err != nil {
		return "", fmt.Errorf("no se pudo comprobar el prefijo de Wine: %w", err)
	}
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
	var err error
	if !strings.EqualFold(filepath.Ext(source), ".exe") {
		return "", errors.New("solo se pueden copiar archivos .exe como portable")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", errors.New("la ruta del ejecutable portable no es válida")
	}
	prefix, err = filepath.Abs(prefix)
	if err != nil {
		return "", errors.New("la ruta del prefijo no es válida")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer el ejecutable portable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("el ejecutable portable no es un archivo regular")
	}
	sourceDir := filepath.Dir(source)
	if err := validateNoSymlinkPath(sourceDir, source); err != nil {
		return "", err
	}
	filename := filepath.Base(source)
	if filename == "." || filename == ".." || filename == "" || filename != filepath.Clean(filename) {
		return "", errors.New("el nombre del ejecutable portable no es válido")
	}
	appDir := filepath.Join(prefix, safeAppDirectory(name))
	destination := filepath.Join(appDir, filename)
	if err := validateNoSymlinkPath(prefix, appDir); err != nil {
		return "", err
	}
	if info, err := os.Lstat(appDir); err == nil && info.IsDir() {
		return "", errors.New("la carpeta de la aplicación portable ya existe")
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("no se pudo validar la carpeta de la aplicación: %w", err)
	}
	rel, err := filepath.Rel(prefix, destination)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("la ruta del ejecutable portable no es segura")
	}
	if pathsOverlap(sourceDir, appDir) {
		return "", errors.New("la carpeta de origen y destino del portable no pueden superponerse")
	}
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar la carpeta de la aplicación: %w", err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		_ = os.RemoveAll(appDir)
		return "", fmt.Errorf("no se pudo leer el ejecutable portable: %w", err)
	}
	mode := info.Mode().Perm() | 0111
	if err := os.WriteFile(destination, data, mode); err != nil {
		_ = os.RemoveAll(appDir)
		return "", fmt.Errorf("no se pudo copiar el ejecutable portable: %w", err)
	}
	return destination, nil
}

func pathsOverlap(first, second string) bool {
	first, _ = filepath.Abs(first)
	second, _ = filepath.Abs(second)
	firstInSecond, _ := filepath.Rel(second, first)
	secondInFirst, _ := filepath.Rel(first, second)
	return firstInSecond == "." || (firstInSecond != ".." && !strings.HasPrefix(firstInSecond, ".."+string(filepath.Separator))) || secondInFirst == "." || (secondInFirst != ".." && !strings.HasPrefix(secondInFirst, ".."+string(filepath.Separator)))
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

func writeAppFiles(home string, req InstallRequest, target, prefix string, installDirs ...string) (string, error) {
	name := applicationName(req.InstallerPath)
	appDir := filepath.Join(prefix, safeAppDirectory(name))
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("no se pudo preparar la configuración: %w", err)
	}
	configType := "portable"
	if strings.EqualFold(filepath.Ext(req.InstallerPath), ".msi") {
		configType = "msi"
	}
	installDir := ""
	if len(installDirs) > 0 {
		installDir = installDirs[0]
	}
	config := appConfig{Name: name, Target: target, InstallDir: installDir, Prefix: prefix, Icon: "application-x-executable", Type: configType}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), append(data, '\n'), 0644); err != nil {
		return "", fmt.Errorf("no se pudo guardar la configuración: %w", err)
	}
	if configType == "portable" {
		if err := writePortableLauncher(home, name, target, filepath.Dir(target)); err != nil {
			_ = os.Remove(filepath.Join(appDir, "config.json"))
			return "", err
		}
	}
	return appDir, nil
}

func applicationsMenuLauncherPath(home, name string) string {
	return filepath.Join(home, ".local", "share", "applications", safeDesktopFilename(name))
}

func writePortableLauncher(home, name, target, workingDir string) error {
	path := applicationsMenuLauncherPath(home, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("no se pudo preparar el menú de aplicaciones: %w", err)
	}
	// Path is a Desktop Entry key, not a shell argument: quoting it makes the
	// quotes part of the directory name for some desktop environments.
	content := "[Desktop Entry]\nType=Application\nName=" + name + "\nExec=env " + desktopExecArg("WINEPREFIX="+filepath.Dir(filepath.Dir(target))) + " wine " + desktopExecArg(target) + "\nPath=" + workingDir + "\nIcon=application-x-executable\nTerminal=false\nX-WindowsInstaller-Managed=true\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("no se pudo crear el acceso del menú de aplicaciones: %w", err)
	}
	return nil
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

func safeMSIInstallDirectory(prefix, target string, configuredDirs ...string) (string, error) {
	installDir := ""
	if len(configuredDirs) > 0 {
		installDir = configuredDirs[0]
	}
	if installDir == "" && target != "" {
		installDir = filepath.Dir(target)
	}
	if strings.TrimSpace(installDir) == "" || !filepath.IsAbs(installDir) || containsControlCharacter(installDir) {
		return "", errors.New("la ruta de instalación MSI no es segura")
	}
	driveRoot := filepath.Join(prefix, "drive_c")
	dirRel, err := filepath.Rel(driveRoot, installDir)
	if err != nil || dirRel == "." || dirRel == ".." || strings.HasPrefix(dirRel, ".."+string(filepath.Separator)) {
		return "", errors.New("la ruta de instalación MSI está fuera del prefijo administrado")
	}
	if dirRel == "." || dirRel == ".." || strings.HasPrefix(dirRel, ".."+string(filepath.Separator)) {
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
	if target != "" {
		rel, err := filepath.Rel(installDir, target)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !strings.EqualFold(filepath.Ext(target), ".exe") {
			return "", errors.New("la ruta de instalación MSI no identifica un ejecutable válido")
		}
		if info, err := os.Lstat(target); err == nil {
			if !info.Mode().IsRegular() {
				return "", errors.New("el ejecutable MSI administrado no es un archivo regular")
			}
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("no se pudo validar el ejecutable MSI administrado: %w", err)
		}
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
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("la raíz de la aplicación contiene un enlace simbólico")
	}
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
		installDir, err = safeMSIInstallDirectory(prefix, config.Target, config.InstallDir)
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
	if appType != "msi" {
		launcher := applicationsMenuLauncherPath(home, config.Name)
		if info, err := os.Lstat(launcher); err == nil {
			if info.Mode().IsRegular() {
				data, readErr := os.ReadFile(launcher)
				expectedExec := "Exec=env " + desktopExecArg("WINEPREFIX="+filepath.Dir(filepath.Dir(config.Target))) + " wine " + desktopExecArg(config.Target)
				if readErr == nil && strings.Contains(string(data), "X-WindowsInstaller-Managed=true") && strings.Contains(string(data), expectedExec) {
					if err := os.Remove(launcher); err != nil {
						return fmt.Errorf("no se pudo quitar el acceso del menú de aplicaciones: %w", err)
					}
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("no se pudo validar el acceso del menú de aplicaciones: %w", err)
		}
	}
	if appType == "msi" {
		if err := os.RemoveAll(appDir); err != nil {
			return fmt.Errorf("no se pudo quitar la configuración: %w", err)
		}
	}
	return nil
}

func discoverInstalledExecutable(prefix string, before map[string]struct{}) (string, int) {
	target, count, _ := discoverInstalledLocation(prefix, before)
	if count != 1 {
		return "", count
	}
	return target, count
}

func discoverInstalledLocation(prefix string, before map[string]struct{}) (string, int, string) {
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
	if len(candidates) > 0 {
		return candidates[0], len(candidates), filepath.Dir(candidates[0])
	}
	return "", 0, ""
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

const maxWineOutput = 4096

type boundedOutput struct {
	mu        sync.Mutex
	data      bytes.Buffer
	truncated bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := maxWineOutput - w.data.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = w.data.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	_, _ = w.data.Write(p)
	return len(p), nil
}

func (w *boundedOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	output := w.data.String()
	if w.truncated {
		output += "\n[Wine output truncated]"
	}
	return output
}

type commandProcess struct {
	cmd    *exec.Cmd
	output *boundedOutput
}

func (p *commandProcess) Wait() error    { return p.cmd.Wait() }
func (p *commandProcess) Output() string { return p.output.String() }

func sanitizeWineOutput(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") {
			lines[i] = "[Wine output redacted]"
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func processFailureMessage(prefix string, process CommandProcess, err error) string {
	message := fmt.Sprintf("%s: %v.", prefix, err)
	if output := sanitizeWineOutput(process.Output()); output != "" {
		message += " Salida de Wine: " + output
	}
	return message
}

func defaultCommandStarterWithEnv(ctx context.Context, env []string, workingDir, name string, args ...string) (CommandProcess, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), env...)
	command.Dir = workingDir
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &commandProcess{cmd: command, output: output}, nil
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
