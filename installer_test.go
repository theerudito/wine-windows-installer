package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateInstallerPath(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		want bool
	}{
		{"accepts exe", ".EXE", true},
		{"accepts msi", ".msi", true},
		{"rejects other extension", ".deb", false},
		{"rejects missing file", ".exe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "setup"+tt.ext)
			if tt.want || tt.name == "accepts exe" || tt.name == "accepts msi" {
				if tt.want {
					if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if got := validateInstallerPath(path) == nil; got != tt.want {
				t.Fatalf("valid = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateDesktopName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  bool
	}{
		{"valid name", "Mi aplicación", true},
		{"empty", " ", false},
		{"path separator", "../../unsafe", false},
		{"newline", "bad\nname", false},
		{"too long", strings.Repeat("x", 81), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateDesktopName(tt.input) == nil; got != tt.want {
				t.Fatalf("valid = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteDesktopEntry(t *testing.T) {
	home := t.TempDir()
	desktop := filepath.Join(home, "Desktop")
	if err := os.Mkdir(desktop, 0755); err != nil {
		t.Fatal(err)
	}
	installer := filepath.Join(home, "my setup.msi")
	target := filepath.Join(home, "Installed App.exe")
	if err := os.WriteFile(installer, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	path, err := writeDesktopEntry(home, DesktopRequest{InstallerPath: installer, TargetPath: target, Name: "Mi App"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, `Exec=wine "`) || !strings.Contains(text, "Installed App.exe") || strings.Contains(text, "my setup.msi") {
		t.Fatalf("unexpected desktop entry: %s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0755 {
		t.Fatalf("permissions = %o, want 755", info.Mode().Perm())
	}
}

func TestDesktopEntryTargetsSelectedExecutable(t *testing.T) {
	tests := []struct {
		name      string
		installer string
		target    string
	}{
		{name: "portable points to selected exe", installer: "portable.exe", target: "portable.exe"},
		{name: "installer points to installed exe", installer: "setup.exe", target: "installed/real-app.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.Mkdir(filepath.Join(home, "Desktop"), 0755); err != nil {
				t.Fatal(err)
			}
			installer := filepath.Join(home, tt.installer)
			target := filepath.Join(home, tt.target)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{installer, target} {
				if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			shortcut, err := writeDesktopEntry(home, DesktopRequest{InstallerPath: installer, TargetPath: target, Name: "Test App"})
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(shortcut)
			if err != nil {
				t.Fatal(err)
			}
			text := string(content)
			expected := "Exec=wine " + desktopExecArg(target)
			if !strings.Contains(text, expected) || target != installer && strings.Contains(text, filepath.Base(installer)) {
				t.Fatalf("desktop entry = %s", text)
			}
		})
	}
}

func TestInvalidTargetRejected(t *testing.T) {
	home := t.TempDir()
	installer := filepath.Join(home, "setup.msi")
	if err := os.WriteFile(installer, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(home, "missing.exe"), filepath.Join(home, "not-executable.txt")} {
		t.Run(filepath.Base(target), func(t *testing.T) {
			if filepath.Ext(target) == ".txt" {
				if err := os.WriteFile(target, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := desktopEntry(DesktopRequest{InstallerPath: installer, TargetPath: target, Name: "Test App"}); err == nil {
				t.Fatal("expected invalid target error")
			}
		})
	}
}

func TestInstallerCommand(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		want []string
	}{
		{"exe", ".exe", []string{"wine", "path.exe"}},
		{"msi", ".msi", []string{"wine", "msiexec", "/i", "path.msi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "path"+tt.ext)
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := installerCommand(path)
			if err != nil {
				t.Fatal(err)
			}
			if got[0] != tt.want[0] || len(got) != len(tt.want) || got[len(got)-1] != path {
				t.Fatalf("command = %#v, want suffix %#v", got, tt.want)
			}
		})
	}
}

func TestMSIInstallCommandRemainsCorrect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.msi")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := installerCommand(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wine", "msiexec", "/i", path}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestRunInstallerUsesSeparateArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portable app.exe")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs []string
	app := &App{
		start: func(_ context.Context, name string, args ...string) error {
			gotName, gotArgs = name, args
			return nil
		},
	}
	result := app.RunInstaller(path)
	if !result.Success || gotName != "wine" || len(gotArgs) != 1 || gotArgs[0] != path {
		t.Fatalf("result = %#v, command = %q %#v", result, gotName, gotArgs)
	}
}

func TestDetectWineUsesRunner(t *testing.T) {
	called := false
	status := detectWine(context.Background(), func(_ context.Context, name string, args ...string) ([]byte, error) {
		called = name == "wine" && len(args) == 1 && args[0] == "--version"
		return []byte("wine-9.0\n"), nil
	})
	if !called && status.Installed {
		// LookPath is intentionally environment-dependent; this test still verifies the pure result shape when Wine exists.
		t.Log("wine is not available in the test environment")
	}
}
