package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxArchiveFileSize = 1 << 30 // 1 GB

// ArchiveDirectory walks srcDir and appends all eligible files to tw under tarPrefix/.
// skipFn, if non-nil, is called with the absolute file path; return true to skip.
// Returns the count and total byte size of files written.
func ArchiveDirectory(tw *tar.Writer, srcDir, tarPrefix string, skipFn func(string) bool) (files int, bytes int64, err error) {
	srcDir = filepath.Clean(srcDir)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			// Non-fatal: skip unreadable entries.
			return nil
		}

		// Skip symlinks.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		name := info.Name()

		// Skip hidden files/dirs.
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip tmp dirs.
		if info.IsDir() && (name == "tmp" || name == ".tmp") {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		// Skip oversized files.
		if info.Size() > maxArchiveFileSize {
			return nil
		}

		// Custom skip predicate.
		if skipFn != nil && skipFn(path) {
			return nil
		}

		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return nil
		}

		tarPath := tarPrefix + "/" + filepath.ToSlash(rel)

		hdr := &tar.Header{
			Name:    tarPath,
			Mode:    int64(info.Mode()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header %s: %w", tarPath, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		n, copyErr := io.Copy(tw, f)
		f.Close()
		if copyErr != nil {
			return fmt.Errorf("copy %s: %w", path, copyErr)
		}

		files++
		bytes += n
		return nil
	})

	return files, bytes, walkErr
}
