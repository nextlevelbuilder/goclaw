package config

import (
	"os"
	"strings"
	"sync"
)

var (
	dockerOnce   sync.Once
	dockerCached bool
)

// InDocker returns true when running inside a Docker container.
// Result is cached after the first call.
func InDocker() bool {
	dockerOnce.Do(func() {
		_, err := os.Stat("/.dockerenv")
		dockerCached = err == nil
	})
	return dockerCached
}

// DockerLocalhost rewrites localhost or 127.0.0.1 in url to host.docker.internal
// when running inside Docker, so the container can reach host services.
// Returns the url unchanged when not in Docker or when it doesn't reference loopback.
func DockerLocalhost(url string) string {
	if !InDocker() {
		return url
	}
	if strings.Contains(url, "localhost") {
		return strings.Replace(url, "localhost", "host.docker.internal", 1)
	}
	if strings.Contains(url, "127.0.0.1") {
		return strings.Replace(url, "127.0.0.1", "host.docker.internal", 1)
	}
	return url
}
