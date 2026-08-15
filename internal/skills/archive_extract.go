package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Sentinel errors for archive extraction.
var (
	ErrUnsafePath     = errors.New("archive: unsafe path rejected")
	ErrZipBomb        = errors.New("archive: uncompressed size exceeds limit (zip bomb protection)")
	ErrFileTooLarge   = errors.New("archive: single file exceeds limit")
	ErrTooManyEntries = errors.New("archive: entry count exceeds limit (DoS protection)")
)

// maxArchiveEntries caps regular file entries per archive. Chosen to cover
// real-world tooling (Go toolchain has ~4k, Python wheel ~6k) while
// preventing a 400M×1-byte file entry DoS where `append` growth + per-entry
// struct alloc (~64B) would OOM the server.
const maxArchiveEntries = 10_000

// ArchiveFile is a single extracted entry held in memory.
type ArchiveFile struct {
	Name    string
	Mode    fs.FileMode
	Size    int64
	Content []byte
}

// Magic byte sequences for format detection.
var (
	magicGzip = []byte{0x1f, 0x8b}
	magicZip  = []byte{0x50, 0x4b, 0x03, 0x04}
	magicELF  = []byte{0x7f, 0x45, 0x4c, 0x46}
)

// ExtractArchive detects the format by magic bytes + extension fallback and extracts.
// maxUncompressed caps total uncompressed bytes (zip-bomb protection).
// Raw (non-archive) inputs are named after filepath.Base(path). Use
// ExtractArchiveAs when the path is a temp file and a logical name should be
// recorded instead.
func ExtractArchive(path string, maxUncompressed int64) ([]ArchiveFile, error) {
	return ExtractArchiveAs(path, "", maxUncompressed)
}

// ExtractArchiveAs is ExtractArchive with a caller-supplied fallbackName for
// raw (non-archive) binary inputs. This matters when `path` is a temp file
// like `/tmp/goclaw-gh-asset-XXXX.bin` — without a logical name the resulting
// ArchiveFile.Name leaks the temp filename into downstream install logic.
// Empty fallbackName falls back to filepath.Base(path).
func ExtractArchiveAs(path, fallbackName string, maxUncompressed int64) ([]ArchiveFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var head [4]byte
	n, _ := io.ReadFull(f, head[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	prefix := head[:n]

	rawName := fallbackName
	if rawName == "" {
		rawName = filepath.Base(path)
	}

	switch {
	case bytes.HasPrefix(prefix, magicGzip):
		return extractTarGz(f, maxUncompressed)
	case bytes.HasPrefix(prefix, magicZip):
		return extractZip(path, maxUncompressed)
	case bytes.HasPrefix(prefix, magicELF):
		return extractRaw(f, rawName, maxUncompressed)
	}
	// Fallback on extension.
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(f, maxUncompressed)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(path, maxUncompressed)
	}
	// Last resort: treat as raw binary.
	return extractRaw(f, rawName, maxUncompressed)
}

// sanitizePath rejects absolute, parent-escaping, and Windows-drive paths.
// Returns a cleaned relative path safe to join with an install dir.
func sanitizePath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: empty", ErrUnsafePath)
	}
	// Reject null bytes explicitly.
	if strings.ContainsRune(name, 0x00) {
		return "", fmt.Errorf("%w: null byte", ErrUnsafePath)
	}
	// Reject Windows drive prefix like "C:\".
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("%w: windows drive %q", ErrUnsafePath, name)
	}
	// Normalize both separators.
	normalized := strings.ReplaceAll(name, "\\", "/")
	// Reject absolute.
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("%w: absolute path %q", ErrUnsafePath, name)
	}
	// Reject any "../" component.
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: traversal component in %q", ErrUnsafePath, name)
		}
	}
	cleaned := path.Clean(normalized)
	// path.Clean of a non-absolute path should remain non-absolute.
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: escapes base after clean %q → %q", ErrUnsafePath, name, cleaned)
	}
	return cleaned, nil
}

// extractTarGz streams a gzip'd tar and returns all regular-file entries.
func extractTarGz(r io.Reader, maxUncompressed int64) ([]ArchiveFile, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	var out []ArchiveFile
	var total int64
	// iterations counts ALL tar headers seen (including symlinks/dirs we
	// skip). A crafted archive can inflate a tiny gzip payload into billions
	// of zero-size symlink headers (header bytes count against
	// maxUncompressed only for regular-file payloads); without a header cap,
	// the loop itself becomes the DoS.
	var iterations int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		iterations++
		if iterations > maxArchiveEntries {
			return nil, ErrTooManyEntries
		}

		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			// supported
		case tar.TypeSymlink, tar.TypeLink:
			slog.Warn("archive: skipping link entry", "name", hdr.Name, "type", string(hdr.Typeflag))
			continue
		default:
			// Skip directories and other special types.
			continue
		}

		clean, err := sanitizePath(hdr.Name)
		if err != nil {
			return nil, err
		}

		if hdr.Size < 0 {
			return nil, fmt.Errorf("tar: negative size for %q", hdr.Name)
		}
		if total+hdr.Size > maxUncompressed {
			return nil, ErrZipBomb
		}
		total += hdr.Size

		buf := make([]byte, 0, hdr.Size)
		w := bytes.NewBuffer(buf)
		// Use limited reader so a truncated tar doesn't spin forever.
		if _, err := io.Copy(w, io.LimitReader(tr, hdr.Size)); err != nil {
			return nil, fmt.Errorf("tar read %q: %w", hdr.Name, err)
		}
		out = append(out, ArchiveFile{
			Name:    clean,
			Mode:    fs.FileMode(hdr.Mode) & fs.ModePerm,
			Size:    hdr.Size,
			Content: w.Bytes(),
		})
	}
	return out, nil
}

// peekZipEntryCount reads only the end-of-central-directory record and
// returns the declared entry count WITHOUT allocating per-entry structs.
// Returns (-1, nil) when EOCD lookup fails gracefully — callers should fall
// back to the stdlib parser which has its own stricter format checks.
//
// This is a critical DoS guard: zip.OpenReader alloc's a []*File of declared
// capacity BEFORE our higher-level pre-check runs. A crafted zip claiming
// 4M entries in a 200MB file could otherwise pin ~1GB of heap per call.
func peekZipEntryCount(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return -1, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return -1, err
	}
	size := st.Size()
	if size < 22 {
		return -1, nil // too small for any valid zip EOCD
	}
	// EOCD record is 22 bytes + variable-length comment (up to 65535 bytes).
	// Scan the last ≤65557 bytes from the back for the EOCD signature.
	const eocdSigMax = 65557
	scanFrom := int64(0)
	if size > eocdSigMax {
		scanFrom = size - eocdSigMax
	}
	buf := make([]byte, size-scanFrom)
	if _, err := f.ReadAt(buf, scanFrom); err != nil && !errors.Is(err, io.EOF) {
		return -1, err
	}
	// EOCD magic: 0x06054b50 (little-endian on-disk: 50 4b 05 06).
	sig := []byte{0x50, 0x4b, 0x05, 0x06}
	for i := len(buf) - 22; i >= 0; i-- {
		if buf[i] == sig[0] && bytes.Equal(buf[i:i+4], sig) {
			// EOCD layout (offsets from record start):
			//  10: total number of entries in central directory (u16)
			// A value of 0xFFFF indicates ZIP64 — we conservatively bail
			// and let the stdlib parser decide (it handles ZIP64 and has
			// its own entries-vs-size sanity check).
			n := binary.LittleEndian.Uint16(buf[i+10 : i+12])
			if n == 0xFFFF {
				return -1, nil
			}
			return int(n), nil
		}
	}
	return -1, nil
}

// extractZip opens a zip file and returns all regular file entries.
func extractZip(filePath string, maxUncompressed int64) ([]ArchiveFile, error) {
	// Pre-check entry count BEFORE zip.OpenReader to prevent the stdlib
	// from pre-allocating [N]*zip.File for a crafted declared count.
	if n, err := peekZipEntryCount(filePath); err == nil && n > maxArchiveEntries {
		return nil, ErrTooManyEntries
	}

	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("zip open: %w", err)
	}
	defer zr.Close()

	// Post-open recheck covers ZIP64 (where peek returns -1 and the stdlib
	// parsed the true count) plus declared-vs-actual mismatches.
	if len(zr.File) > maxArchiveEntries {
		return nil, ErrTooManyEntries
	}
	var sum uint64
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		sum += f.UncompressedSize64
		if sum > uint64(maxUncompressed) {
			return nil, ErrZipBomb
		}
	}

	var out []ArchiveFile
	var total int64
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		if f.Mode()&fs.ModeSymlink != 0 {
			slog.Warn("archive: skipping symlink entry", "name", f.Name)
			continue
		}
		// Guard against exhausting the cap through streaming reads alone
		// (pre-check above covers declared counts — this covers runtime).
		if total >= maxUncompressed {
			return nil, ErrZipBomb
		}
		clean, err := sanitizePath(f.Name)
		if err != nil {
			return nil, err
		}

		if f.UncompressedSize64 > uint64(maxUncompressed) {
			return nil, ErrFileTooLarge
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("zip open %q: %w", f.Name, err)
		}

		// Also enforce streaming cap in case declared size lies.
		lr := io.LimitReader(rc, maxUncompressed-total+1)
		buf, err := io.ReadAll(lr)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("zip read %q: %w", f.Name, err)
		}
		if int64(len(buf)) > maxUncompressed-total {
			return nil, ErrZipBomb
		}
		total += int64(len(buf))

		out = append(out, ArchiveFile{
			Name:    clean,
			Mode:    f.Mode().Perm(),
			Size:    int64(len(buf)),
			Content: buf,
		})
	}
	return out, nil
}

// extractRaw reads the entire file as a single binary entry.
// maxBytes guards against oversized inputs (belt-and-braces with the downloader
// cap) so this helper is safe to call from any context.
func extractRaw(f *os.File, name string, maxBytes int64) ([]ArchiveFile, error) {
	if maxBytes <= 0 {
		maxBytes = 200 * 1024 * 1024
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrFileTooLarge
	}
	clean, err := sanitizePath(name)
	if err != nil {
		return nil, err
	}
	return []ArchiveFile{{
		Name:    clean,
		Mode:    0o755,
		Size:    int64(len(b)),
		Content: b,
	}}, nil
}
