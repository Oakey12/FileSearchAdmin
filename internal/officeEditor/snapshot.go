package officeeditor

import (
	"fmt"
	"os"
	"time"
)

type fileSnapshot struct {
	size     int64
	modified time.Time
}

func captureSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	if info.IsDir() {
		return fileSnapshot{}, fmt.Errorf("выбран путь папки, а не документа")
	}
	return fileSnapshot{size: info.Size(), modified: info.ModTime()}, nil
}

func (snapshot fileSnapshot) verify(path string) error {
	current, err := captureSnapshot(path)
	if err != nil {
		return err
	}
	if current.size != snapshot.size || !current.modified.Equal(snapshot.modified) {
		return fmt.Errorf("исходный документ был изменён после открытия; откройте его повторно")
	}
	return nil
}
