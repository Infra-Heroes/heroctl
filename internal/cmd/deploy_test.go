package cmd

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestDetectContainerEngine(t *testing.T) {
	tests := []struct {
		name           string
		mockLookPath   func(file string) (string, error)
		expectedEngine string
		expectedError  bool
	}{
		{
			name: "docker and podman both available (prefers docker)",
			mockLookPath: func(file string) (string, error) {
				return "/usr/bin/" + file, nil
			},
			expectedEngine: "docker",
			expectedError:  false,
		},
		{
			name: "only podman available",
			mockLookPath: func(file string) (string, error) {
				if file == "podman" {
					return "/usr/bin/podman", nil
				}
				return "", &exec.Error{Name: file, Err: errors.New("not found")}
			},
			expectedEngine: "podman",
			expectedError:  false,
		},
		{
			name: "neither available",
			mockLookPath: func(file string) (string, error) {
				return "", &exec.Error{Name: file, Err: errors.New("not found")}
			},
			expectedEngine: "",
			expectedError:  true,
		},
	}

	// Save original function and restore after tests
	originalLookPathFunc := lookPathFunc
	defer func() { lookPathFunc = originalLookPathFunc }()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookPathFunc = tc.mockLookPath

			engine, err := detectContainerEngine()

			if tc.expectedError && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tc.expectedError && err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			if engine != tc.expectedEngine {
				t.Errorf("expected engine %q, got %q", tc.expectedEngine, engine)
			}
		})
	}
}

// Registry credentials from hero-api live five minutes, so the login has to
// happen after the build: a slow build used to push with an expired token.
func TestBuildLoginPushLogsInAfterBuild(t *testing.T) {
	var calls []string
	orig := runEngine
	defer func() { runEngine = orig }()
	runEngine = func(_ context.Context, _, _ string, args ...string) error {
		calls = append(calls, args[0])
		return nil
	}

	login := func() error { calls = append(calls, "login"); return nil }
	if err := buildLoginPush(context.Background(), "docker", "reg/org/app:abc", login); err != nil {
		t.Fatalf("buildLoginPush: %v", err)
	}

	want := []string{"build", "login", "push"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("engine calls = %v, want %v", calls, want)
	}
}

func TestBuildLoginPushSkipsLoginWhenBuildFails(t *testing.T) {
	orig := runEngine
	defer func() { runEngine = orig }()
	runEngine = func(_ context.Context, _, _ string, args ...string) error {
		if args[0] == "build" {
			return errors.New("boom")
		}
		t.Fatalf("unexpected engine call %v after a failed build", args)
		return nil
	}

	loggedIn := false
	err := buildLoginPush(context.Background(), "docker", "reg/org/app:abc", func() error { loggedIn = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "docker build") {
		t.Fatalf("err = %v, want docker build error", err)
	}
	if loggedIn {
		t.Fatal("logged in although the build failed")
	}
}
