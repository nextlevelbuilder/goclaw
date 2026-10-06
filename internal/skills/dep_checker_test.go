package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// writeFakeBin puts an executable shell script named name in a temp dir and
// makes that dir the only entry on PATH.
func writeFakeBin(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell-script binaries are not supported on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// A probe killed by the timeout must not report the packages as missing.
// exec replaces the shell so the kill on deadline closes stdout; sleep is
// called by absolute path because PATH holds only the fake binary.
func TestCheckPythonPackagesTimeoutIsNotMissing(t *testing.T) {
	writeFakeBin(t, "python3", "#!/bin/sh\nexec /bin/sleep 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	if got := checkPythonPackages([]string{"anthropic"}, ""); len(got) != 0 {
		t.Fatalf("timed-out check reported missing packages: got %v, want none", got)
	}
}

// Genuine import failures must still surface.
func TestCheckPythonPackagesReportsImportError(t *testing.T) {
	writeFakeBin(t, "python3", "#!/bin/sh\necho yaml\n")

	got := checkPythonPackages([]string{"yaml"}, "")
	if want := []string{"pip:pyyaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCheckNodePackagesTimeoutIsNotMissing(t *testing.T) {
	writeFakeBin(t, "node", "#!/bin/sh\nexec /bin/sleep 7\n")
	t.Setenv("GOCLAW_DEP_CHECK_TIMEOUT", "1s")

	if got := checkNodePackages([]string{"left-pad"}, ""); len(got) != 0 {
		t.Fatalf("timed-out check reported missing packages: got %v, want none", got)
	}
}

func TestImportToPipName(t *testing.T) {
	cases := []struct {
		importName string
		want       string
	}{
		{"cv2", "opencv-python"},
		{"PIL", "Pillow"},
		{"yaml", "pyyaml"},
		{"sklearn", "scikit-learn"},
		{"bs4", "beautifulsoup4"},
		{"dateutil", "python-dateutil"},
		{"dotenv", "python-dotenv"},
		{"pptx", "python-pptx"},
		{"docx", "python-docx"},
		{"attr", "attrs"},
		{"gi", "PyGObject"},
		{"psycopg2", "psycopg2-binary"},
		{"psycopg", "psycopg[binary]"},
		{"MySQLdb", "mysqlclient"},
		{"Crypto", "pycryptodome"},
		{"serial", "pyserial"},
		{"skimage", "scikit-image"},
		{"Levenshtein", "python-Levenshtein"},
		{"requests", "requests"},
		{"numpy", "numpy"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.importName, func(t *testing.T) {
			if got := importToPipName(tc.importName); got != tc.want {
				t.Errorf("importToPipName(%q) = %q, want %q", tc.importName, got, tc.want)
			}
		})
	}
}
