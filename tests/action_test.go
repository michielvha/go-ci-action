package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

type step struct {
	Name            string `yaml:"name"`
	Run             string `yaml:"run"`
	ContinueOnError bool   `yaml:"continue-on-error"`
}

type result struct {
	code   int
	stdout string
	stderr string
}

func TestGoChecks(t *testing.T) {
	for _, tool := range []string{"go", "gofmt", "golangci-lint", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []step `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(raw, &action); err != nil {
		t.Fatal(err)
	}
	checks := make(map[string]step)
	for _, s := range action.Runs.Steps {
		if s.Run != "" {
			checks[s.Name] = s
		}
	}
	if len(checks) == 0 {
		t.Fatal("the action declares no commands to test")
	}
	run := func(t *testing.T, directory, name string) result {
		t.Helper()
		s, ok := checks[name]
		if !ok {
			t.Fatalf("the action has no %s check", name)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", "-e", "-o", "pipefail", "-c", s.Run)
		cmd.Dir = directory
		cmd.Env = append(os.Environ(), "LINT_CONFIG=.golangci.yml", "GOPROXY=off")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("%s timed out: %v", name, ctx.Err())
		}
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		return result{code, stdout.String(), stderr.String()}
	}

	t.Run("valid source passes without creating build artifacts", func(t *testing.T) {
		directory := fixture(t)
		before := filenames(t, directory)
		for _, s := range action.Runs.Steps {
			if s.Run == "" {
				continue
			}
			if got := run(t, directory, s.Name); got.code != 0 {
				t.Fatalf("%s failed:\n%s%s", s.Name, got.stdout, got.stderr)
			}
		}
		if !slices.Equal(filenames(t, directory), before) {
			t.Fatal("CI left build artifacts in the checkout")
		}
	})
	t.Run("unformatted source fails formatting", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main.go", "package main\nfunc main(){ }\n")
		if run(t, directory, "Format").code == 0 {
			t.Fatal("formatting accepted unformatted source")
		}
	})
	t.Run("a gofmt error cannot become an empty successful check", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main.go", "not valid Go")
		if run(t, directory, "Format").code == 0 {
			t.Fatal("formatting ignored a gofmt failure")
		}
	})
	t.Run("an untidy module fails without modification", func(t *testing.T) {
		directory := fixture(t)
		source := read(t, directory, "go.mod") +
			"\nrequire example.com/unused v0.0.0\nreplace example.com/unused => ./unused\n"
		write(t, directory, "go.mod", source)
		write(t, directory, "unused/go.mod", "module example.com/unused\n\ngo 1.27.2\n")
		got := run(t, directory, "Tidy modules")
		if got.code == 0 || !strings.Contains(got.stdout, "-require example.com/unused") {
			t.Fatalf("expected a tidy diff, got %d:\n%s%s", got.code, got.stdout, got.stderr)
		}
		if read(t, directory, "go.mod") != source {
			t.Fatal("the tidy check changed go.mod")
		}
	})
	t.Run("vet findings fail CI", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main.go", strings.ReplaceAll(read(t, directory, "main.go"),
			"fmt.Println(greeting())", `fmt.Printf("%d", "wrong")`))
		got := run(t, directory, "Vet")
		if got.code == 0 || !strings.Contains(got.stderr, "fmt.Printf") {
			t.Fatalf("expected a vet finding, got %d:\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("a failing test fails CI", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main_test.go",
			"package main\nimport \"testing\"\nfunc TestFailure(t *testing.T) { t.Fatal(\"expected fixture failure\") }\n")
		got := run(t, directory, "Test")
		if got.code == 0 || !strings.Contains(got.stdout, "expected fixture failure") {
			t.Fatalf("expected the fixture test to fail, got %d:\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("a build error fails CI", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main.go", "package main\nfunc main() { undefined() }\n")
		if run(t, directory, "Build").code == 0 {
			t.Fatal("the build check accepted invalid source")
		}
	})
	t.Run("invalid lint configuration fails CI", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, ".golangci.yml", "version: \"invalid\"\n")
		if run(t, directory, "Verify lint configuration").code == 0 {
			t.Fatal("invalid lint configuration was accepted")
		}
	})
	t.Run("lint findings fail even when vet and tests pass", func(t *testing.T) {
		directory := fixture(t)
		write(t, directory, "main.go", strings.ReplaceAll(read(t, directory, "main.go"),
			"fmt.Println(greeting())", "value := 1\n\tvalue = 2\n\tfmt.Println(value)"))
		write(t, directory, ".golangci.yml", "version: \"2\"\nlinters:\n  default: none\n  enable: [ineffassign]\n")
		for _, name := range []string{"Vet", "Test"} {
			if got := run(t, directory, name); got.code != 0 {
				t.Fatalf("%s unexpectedly failed:\n%s%s", name, got.stdout, got.stderr)
			}
		}
		got := run(t, directory, "Lint")
		if got.code == 0 || !strings.Contains(got.stdout, "ineffassign") {
			t.Fatalf("expected a lint-only finding, got %d:\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("command failures cannot be advisory", func(t *testing.T) {
		for _, s := range checks {
			if s.ContinueOnError {
				t.Errorf("%s permits a successful action after a failed check", s.Name)
			}
		}
	})
}

func fixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.CopyFS(directory, os.DirFS("fixture")); err != nil {
		t.Fatal(err)
	}
	return directory
}

func read(t *testing.T, directory, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func write(t *testing.T, directory, name, content string) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func filenames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
