// Package storage хранит видеофайлы на локальном диске (в Docker — в volume).
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type LocalStorage struct {
	dir string
}

func NewLocalStorage(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &LocalStorage{dir: dir}, nil
}

// Save записывает поток в файл со случайным именем и возвращает имя и размер.
// Случайное имя исключает конфликты и не даёт клиенту влиять на путь к файлу.
func (s *LocalStorage) Save(src io.Reader, ext string) (string, int64, error) {
	name, err := randomName()
	if err != nil {
		return "", 0, err
	}
	name += ext

	path := filepath.Join(s.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, fmt.Errorf("create file: %w", err)
	}

	size, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return "", 0, fmt.Errorf("write file: %w", copyErr)
		}
		return "", 0, fmt.Errorf("close file: %w", closeErr)
	}
	return name, size, nil
}

func (s *LocalStorage) Open(name string) (*os.File, error) {
	return os.Open(filepath.Join(s.dir, filepath.Base(name)))
}

func (s *LocalStorage) Remove(name string) error {
	err := os.Remove(filepath.Join(s.dir, filepath.Base(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func randomName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
