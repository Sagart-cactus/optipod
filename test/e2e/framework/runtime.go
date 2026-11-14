package framework

import "os"

// ContainerRuntime keeps Kind and image operations on the same explicitly
// selected engine. Docker remains the CI default; Podman needs no daemon shim.
func ContainerRuntime() string {
	if os.Getenv("KIND_EXPERIMENTAL_PROVIDER") == podmanCommand {
		return podmanCommand
	}
	return "docker"
}
