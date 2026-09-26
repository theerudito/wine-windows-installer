package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerCommand(t *testing.T) {
	for _, tt := range []struct {
		name, ext string
		want      []string
	}{
		{"exe", ".exe", []string{"wine", "path.exe"}},
		{"msi", ".msi", []string{"wine", "msiexec", "/i", "path.msi"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "path"+tt.ext)
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := installerCommand(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\x00") != strings.Join(append(tt.want[:len(tt.want)-1], path), "\x00") {
				t.Fatalf("command = %#v", got)
			}
		})
	}
}

func TestDiscoverInstalledExecutable(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "Acme.exe")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, count := discoverInstalledExecutable(prefix, map[string]struct{}{})
	if got != path || count != 1 {
		t.Fatalf("got %q, count %d", got, count)
	}
}

func TestDiscoverInstalledExecutableRejectsAmbiguousResults(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "drive_c", "Program Files", "Acme")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.exe", "two.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, count := discoverInstalledExecutable(prefix, map[string]struct{}{})
	if got != "" || count != 2 {
		t.Fatalf("got %q, count %d", got, count)
	}
}

func TestWriteAppFilesPersistsIconAndDesktop(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "programas")
	target := filepath.Join(prefix, "drive_c", "Program Files", "App", "app.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	icon := filepath.Join(home, "original.png")
	if err := os.WriteFile(icon, []byte("icon"), 0600); err != nil {
		t.Fatal(err)
	}
	req := InstallRequest{InstallerPath: filepath.Join(home, "setup.exe"), Name: "Mi App", IconPath: icon}
	if err := os.WriteFile(req.InstallerPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	shortcut, err := writeAppFiles(home, req, target, prefix)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(shortcut)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "Icon="+filepath.Join(prefix, "mi-app", "icon.png")) {
		t.Fatalf("desktop entry = %s", text)
	}
	if _, err := os.Stat(filepath.Join(prefix, "mi-app", "config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Desktop", "mi-app.desktop")); !os.IsNotExist(err) {
		t.Fatalf("unexpected Desktop shortcut state: %v", err)
	}
}

type fakeProcess struct{}

func (fakeProcess) Wait() error { return nil }

func TestInstallUsesWinePrefixAndMSIArguments(t *testing.T) {
	home := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = old })
	path := filepath.Join(home, "setup.msi")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs, gotEnv []string
	app := &App{startEnv: func(_ context.Context, env []string, name string, args ...string) (CommandProcess, error) {
		gotEnv, gotName, gotArgs = env, name, args
		return fakeProcess{}, nil
	}, emit: func(context.Context, string, ...interface{}) {}}
	result := app.Install(InstallRequest{InstallerPath: path, Name: "Setup"})
	if !result.Success {
		t.Fatalf("result = %#v", result)
	}
	if gotName != "wine" || strings.Join(gotArgs, "\x00") != strings.Join([]string{"msiexec", "/i", path}, "\x00") || gotEnv[0] != "WINEPREFIX="+filepath.Join(home, "programas") {
		t.Fatalf("command=%s %#v env=%#v", gotName, gotArgs, gotEnv)
	}
}

func TestFrontendHasNoSecondaryExecutableSelector(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"SelectExecutable", "Elegir archivo", "Ejecutable instalado", "Definí cómo usarla", "Aplicación portable", "Instalador"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("frontend contains forbidden text %q", forbidden)
		}
	}
}
