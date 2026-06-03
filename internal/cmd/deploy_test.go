package cmd

import (
	"errors"
	"os/exec"
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
