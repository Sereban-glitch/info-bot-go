package handlers

import (
	"os"
	"strings"
	"testing"
)

// Фикс A2 (аудит №22): раньше все PDF генерировались в один файл
// /tmp/foi_request.pdf — два одновременных запроса могли подменить
// вложение друг друга. Теперь каждый запрос получает уникальный путь.
func TestFOITempPathUnique(t *testing.T) {
	path1, cleanup1, err := newFOITempPath()
	if err != nil {
		t.Fatalf("newFOITempPath: %v", err)
	}
	defer cleanup1()

	path2, cleanup2, err := newFOITempPath()
	if err != nil {
		cleanup1()
		t.Fatalf("newFOITempPath: %v", err)
	}
	defer cleanup2()

	if path1 == path2 {
		t.Fatalf("временные пути совпали: %s — гонка PDF не закрыта", path1)
	}
	if !strings.Contains(path1, "foi_") || !strings.Contains(path2, "foi_") {
		t.Errorf("пути должны содержать префикс foi_: %s / %s", path1, path2)
	}

	// Старый общий путь больше не используется нигде в коде.
	for _, p := range []string{path1, path2} {
		if p == "/tmp/foi_request.pdf" {
			t.Errorf("используется старый фиксированный путь: %s", p)
		}
	}

	// cleanup действительно удаляет файл.
	cleanup1()
	if _, err := os.Stat(path1); !os.IsNotExist(err) {
		t.Errorf("cleanup не удалил файл %s", path1)
	}
}
