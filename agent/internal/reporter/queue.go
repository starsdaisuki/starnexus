package reporter

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DiskQueue stores metric reports in FIFO order so short primary outages do
// not erase time-series data.
type DiskQueue struct {
	path       string
	maxReports int
	mu         sync.Mutex
}

func NewDiskQueue(path string, maxReports int) (*DiskQueue, error) {
	if path == "" {
		return nil, errors.New("queue path is empty")
	}
	if maxReports < 1 {
		return nil, errors.New("queue max reports must be at least 1")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create queue directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create queue file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close queue file: %w", err)
	}
	return &DiskQueue{path: path, maxReports: maxReports}, nil
}

func (q *DiskQueue) Enqueue(report Report) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	reports, _, err := q.loadLocked()
	if err != nil {
		return err
	}
	reports = append(reports, report)
	if len(reports) > q.maxReports {
		reports = reports[len(reports)-q.maxReports:]
	}
	return q.writeLocked(reports)
}

func (q *DiskQueue) LoadBatch(limit int) ([]Report, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	reports, skipped, err := q.loadLocked()
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		if err := q.writeLocked(reports); err != nil {
			return nil, err
		}
	}
	if limit > 0 && len(reports) > limit {
		reports = reports[:limit]
	}
	return reports, nil
}

func (q *DiskQueue) DropFirst(count int) error {
	if count <= 0 {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	reports, _, err := q.loadLocked()
	if err != nil {
		return err
	}
	if count >= len(reports) {
		return q.writeLocked(nil)
	}
	return q.writeLocked(reports[count:])
}

func (q *DiskQueue) Count() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	reports, skipped, err := q.loadLocked()
	if err != nil {
		return 0, err
	}
	if skipped > 0 {
		if err := q.writeLocked(reports); err != nil {
			return 0, err
		}
	}
	return len(reports), nil
}

func (q *DiskQueue) loadLocked() ([]Report, int, error) {
	file, err := os.Open(q.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open queue: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var reports []Report
	skipped := 0
	for scanner.Scan() {
		var report Report
		if err := json.Unmarshal(scanner.Bytes(), &report); err != nil {
			skipped++
			continue
		}
		reports = append(reports, report)
	}
	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("scan queue: %w", err)
	}
	if len(reports) > q.maxReports {
		reports = reports[len(reports)-q.maxReports:]
		skipped++
	}
	return reports, skipped, nil
}

func (q *DiskQueue) writeLocked(reports []Report) error {
	tmp := q.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open queue temp file: %w", err)
	}
	encoder := json.NewEncoder(file)
	for _, report := range reports {
		if err := encoder.Encode(report); err != nil {
			_ = file.Close()
			return fmt.Errorf("encode queue: %w", err)
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync queue: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close queue temp file: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("replace queue: %w", err)
	}
	return nil
}
