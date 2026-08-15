package http

import (
	"archive/zip"
	"errors"
	"io"
	"os"
)

const maxSkillMarkdownBytes = 100 << 10

var errSkillMarkdownTooLarge = errors.New("SKILL.md exceeds 100 KB limit")

func readZipFile(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, maxSkillMarkdownBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxSkillMarkdownBytes {
		return "", errSkillMarkdownTooLarge
	}
	return string(data), nil
}

func copyZipFileToPath(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, rc)
	return err
}
