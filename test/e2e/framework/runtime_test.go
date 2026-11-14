package framework

import (
	"context"
	"errors"
	"testing"
)

func TestPodmanPreflightChecksSelectedRuntime(t *testing.T) {
	t.Setenv("KIND_EXPERIMENTAL_PROVIDER", "podman")
	if ContainerRuntime() != "podman" || NewImageBuilder("/repo").DockerPath != "podman" {
		t.Fatal("runtime selection differs")
	}
	p := okPreflighter()
	p.ContainerTool = "podman"
	p.LookPath = func(name string) (string, error) {
		if name == "podman" {
			return "", errors.New("missing")
		}
		return "/bin/" + name, nil
	}
	assertBlocker(t, p.Check(context.Background()), "podman-binary")
}
