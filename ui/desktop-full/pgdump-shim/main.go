//go:build windows

// Command pg_dump forwards a pg_dump call into the PostgreSQL container, so the
// built-in `goclaw backup` works on a machine without PostgreSQL client tools
// and always uses a pg_dump that matches the server version.
//
// The caller (internal/backup) passes --host/--port of the host-published port
// and a PGPASSFILE; inside the container the server listens on 127.0.0.1:5432
// and the password travels to docker through the environment, never argv.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultContainer = "goclaw-postgres"
	containerHost    = "127.0.0.1"
	containerPort    = "5432"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	container := os.Getenv("GOCLAW_PG_CONTAINER")
	if container == "" {
		container = defaultContainer
	}
	user, db := optionValue(args, "--username", "-U"), optionValue(args, "--dbname", "-d")
	password := ""
	if path := os.Getenv("PGPASSFILE"); path != "" {
		password = passwordFromPgpass(path, user, db)
	}
	cmd := exec.Command("docker", dockerArgs(container, args)...)
	cmd.Env = os.Environ()
	if password != "" {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+password)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "pg_dump shim: cannot run docker:", err)
		return 127
	}
	return 0
}

// dockerArgs builds the docker invocation. The password is passed by name only.
func dockerArgs(container string, args []string) []string {
	out := []string{"exec", "-e", "PGPASSWORD", container, "pg_dump"}
	return append(out, rewriteConnection(args)...)
}

// rewriteConnection points --host/--port (also -h/-p and --opt=value forms) at
// the container-local server and leaves every other argument untouched.
func rewriteConnection(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--host" || a == "-h":
			out = append(out, a, containerHost)
			i++
		case a == "--port" || a == "-p":
			out = append(out, a, containerPort)
			i++
		case strings.HasPrefix(a, "--host="):
			out = append(out, "--host="+containerHost)
		case strings.HasPrefix(a, "--port="):
			out = append(out, "--port="+containerPort)
		default:
			out = append(out, a)
		}
	}
	return out
}

func optionValue(args []string, names ...string) string {
	for i, a := range args {
		for _, n := range names {
			if a == n && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, n+"=") {
				return strings.TrimPrefix(a, n+"=")
			}
		}
	}
	return ""
}

// passwordFromPgpass returns the password of the first pgpass line matching the
// user and database (or a wildcard), or "".
func passwordFromPgpass(path, user, db string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitPgpass(line)
		if len(fields) != 5 {
			continue
		}
		if (fields[2] == "*" || fields[2] == db) && (fields[3] == "*" || fields[3] == user) {
			return fields[4]
		}
	}
	return ""
}

// splitPgpass splits on unescaped colons and resolves \: and \\ escapes.
func splitPgpass(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == ':':
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(fields, cur.String())
}
