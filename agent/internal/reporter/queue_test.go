package reporter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDiskQueueKeepsNewestReportsWithinCapacity(t *testing.T) {
	queue, err := NewDiskQueue(filepath.Join(t.TempDir(), "queue.jsonl"), 2)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}

	for i := int64(1); i <= 3; i++ {
		if err := queue.Enqueue(Report{CollectedAt: i, NodeID: "node"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	reports, err := queue.LoadBatch(10)
	if err != nil {
		t.Fatalf("load batch: %v", err)
	}
	if len(reports) != 2 || reports[0].CollectedAt != 2 || reports[1].CollectedAt != 3 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
}

func TestDiskQueueDropsSentPrefix(t *testing.T) {
	queue, err := NewDiskQueue(filepath.Join(t.TempDir(), "queue.jsonl"), 10)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	for i := int64(1); i <= 3; i++ {
		if err := queue.Enqueue(Report{CollectedAt: i, NodeID: "node"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	if err := queue.DropFirst(2); err != nil {
		t.Fatalf("drop first: %v", err)
	}
	reports, err := queue.LoadBatch(10)
	if err != nil {
		t.Fatalf("load batch: %v", err)
	}
	if len(reports) != 1 || reports[0].CollectedAt != 3 {
		t.Fatalf("unexpected reports after drop: %+v", reports)
	}
}

func TestDiskQueueSkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	if err := os.WriteFile(path, []byte("{bad json}\n{\"collected_at\":7,\"node_id\":\"node\"}\n"), 0o600); err != nil {
		t.Fatalf("write queue: %v", err)
	}
	queue, err := NewDiskQueue(path, 10)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}

	reports, err := queue.LoadBatch(10)
	if err != nil {
		t.Fatalf("load batch: %v", err)
	}
	if len(reports) != 1 || reports[0].CollectedAt != 7 {
		t.Fatalf("unexpected reports: %+v", reports)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if bytes.Contains(data, []byte("{bad json}")) {
		t.Fatalf("corrupt line was not compacted, got %q", string(data))
	}
}
