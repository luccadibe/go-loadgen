package go_loadgen

import (
	"compress/gzip"
	"encoding/gob"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test data structure that implements CSVWritable
type testCSVData struct {
	ID      int
	Message string
	Value   float64
}

func (t testCSVData) CSVHeaders() []string {
	return []string{"id", "message", "value"}
}

func (t testCSVData) CSVRecord() []string {
	return []string{strconv.Itoa(t.ID), t.Message, strconv.FormatFloat(t.Value, 'f', 2, 64)}
}

func TestNewCSVCollector(t *testing.T) {
	filename := "test_collector.csv"
	defer os.Remove(filename) // Clean up

	collector, err := NewCSVCollector[testCSVData](filename, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}
	defer collector.Close()

	if collector.flushInterval != 100*time.Millisecond {
		t.Errorf("Expected flush interval 100ms, got: %v", collector.flushInterval)
	}
}

func TestNewCSVCollector_InvalidFile(t *testing.T) {
	// Try to create collector with invalid path
	_, err := NewCSVCollector[testCSVData]("/invalid/path/test.csv", time.Second)
	if err == nil {
		t.Error("Expected error for invalid file path, got nil")
	}
}

func TestCSVCollector_Collect(t *testing.T) {
	filename := "test_collect.csv"
	defer os.Remove(filename)

	collector, err := NewCSVCollector[testCSVData](filename, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}

	// Collect some data
	data1 := testCSVData{ID: 1, Message: "test1", Value: 1.23}
	data2 := testCSVData{ID: 2, Message: "test2", Value: 4.56}

	collector.Collect(data1)
	collector.Collect(data2)

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	collector.Close()

	// Read file and verify content
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read CSV file: %v", err)
	}

	contentStr := string(content)
	lines := strings.Split(strings.TrimSpace(contentStr), "\n")

	// Should have header + 2 data lines
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines (header + 2 data), got: %d", len(lines))
	}

	// Check header
	expectedHeader := "id,message,value"
	if lines[0] != expectedHeader {
		t.Errorf("Expected header '%s', got: '%s'", expectedHeader, lines[0])
	}

	// Check data lines
	expectedData1 := "1,test1,1.23"
	expectedData2 := "2,test2,4.56"

	if lines[1] != expectedData1 {
		t.Errorf("Expected data line '%s', got: '%s'", expectedData1, lines[1])
	}
	if lines[2] != expectedData2 {
		t.Errorf("Expected data line '%s', got: '%s'", expectedData2, lines[2])
	}
}

func TestCSVCollector_ConcurrentCollect(t *testing.T) {
	filename := "test_concurrent.csv"
	defer os.Remove(filename)

	collector, err := NewCSVCollector[testCSVData](filename, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}

	// Collect data concurrently from multiple goroutines
	const numGoroutines = 10
	const itemsPerGoroutine = 9

	var wg sync.WaitGroup

	for i := range numGoroutines {
		wg.Go(func() {
			for j := range itemsPerGoroutine {
				data := testCSVData{
					ID:      i*itemsPerGoroutine + j,
					Message: "",
					Value:   float64(i + j),
				}
				collector.Collect(data)
			}
		})
	}

	wg.Wait()

	// Wait for final flush
	time.Sleep(100 * time.Millisecond)
	collector.Close()

	// Read file and verify we have all the data
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read CSV file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Should have header + (numGoroutines * itemsPerGoroutine) data lines
	expectedLines := 1 + (numGoroutines * itemsPerGoroutine)
	if len(lines) != expectedLines {
		t.Errorf("Expected %d lines, got: %d", expectedLines, len(lines))
	}

	// Verify all data is present (check IDs)
	foundIDs := make(map[int]bool)
	for i := 1; i < len(lines); i++ { // Skip header
		parts := strings.Split(lines[i], ",")
		if len(parts) >= 1 {
			id, err := strconv.Atoi(parts[0])
			if err == nil {
				foundIDs[id] = true
			}
		}
	}

	// Should have found all IDs from 0 to (numGoroutines * itemsPerGoroutine - 1)
	for i := range numGoroutines * itemsPerGoroutine {
		if !foundIDs[i] {
			t.Errorf("Missing ID %d in CSV output", i)
		}
	}
}

func TestCSVCollector_FlushInterval(t *testing.T) {
	filename := "test_flush.csv"
	defer os.Remove(filename)

	// Use a longer flush interval
	collector, err := NewCSVCollector[testCSVData](filename, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}

	// Collect one item
	data := testCSVData{ID: 1, Message: "flush_test", Value: 1.0}
	collector.Collect(data)

	// Check that file doesn't exist or is empty immediately
	time.Sleep(50 * time.Millisecond) // Less than flush interval

	if _, err := os.Stat(filename); err == nil {
		// File exists, check if it has content
		content, _ := os.ReadFile(filename)
		if len(content) > 0 {
			// If file has content, it should at least have the header
			lines := strings.Split(strings.TrimSpace(string(content)), "\n")
			if len(lines) > 1 {
				t.Error("Data was flushed before flush interval")
			}
		}
	}

	// Wait for flush interval
	time.Sleep(200 * time.Millisecond)

	// Now file should have content
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read CSV file after flush interval: %v", err)
	}

	if len(content) == 0 {
		t.Error("File is empty after flush interval")
	}

	collector.Close()
}

func TestCSVCollector_Close(t *testing.T) {
	filename := "test_close.csv"
	defer os.Remove(filename)

	collector, err := NewCSVCollector[testCSVData](filename, 1*time.Second) // Long interval
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}

	// Collect data
	data := testCSVData{ID: 1, Message: "close_test", Value: 1.0}
	collector.Collect(data)

	// should flush remaining data
	collector.Close()

	// File should have the data even though flush interval hasn't passed
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read CSV file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 { // Header + 1 data line
		t.Errorf("Expected 2 lines after close, got: %d", len(lines))
	}
}

func TestCSVCollector_MultipleClose(t *testing.T) {
	filename := "test_multiple_close.csv"
	defer os.Remove(filename)

	collector, err := NewCSVCollector[testCSVData](filename, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create CSV collector: %v", err)
	}

	// Close multiple times (should not panic)
	collector.Close()
	collector.Close()
	collector.Close()

	// Should not crash or cause issues
}

func TestGobCollector_Collect(t *testing.T) {
	filename := "test_collect.gob"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create gob collector: %v", err)
	}

	data := []testCSVData{
		{ID: 1, Message: "test1", Value: 1.23},
		{ID: 2, Message: "test2", Value: 4.56},
	}
	for _, item := range data {
		collector.Collect(item)
	}
	collector.Close()

	if err := collector.Err(); err != nil {
		t.Fatalf("Gob collector returned error: %v", err)
	}

	got := readGobRecords[testCSVData](t, filename, false)
	if len(got) != len(data) {
		t.Fatalf("Expected %d records, got %d", len(data), len(got))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Errorf("Expected record %d to be %+v, got %+v", i, data[i], got[i])
		}
	}
}

func TestNewGobCollector_InvalidFlushInterval(t *testing.T) {
	filename := "test_invalid_flush.gob"
	defer os.Remove(filename)

	if _, err := NewGobCollector[testCSVData](filename, 0); err == nil {
		t.Fatal("Expected error for zero flush interval, got nil")
	}
	if _, err := NewGobCollector[testCSVData](filename, -time.Second); err == nil {
		t.Fatal("Expected error for negative flush interval, got nil")
	}
}

func TestGobCollector_GzipCollect(t *testing.T) {
	filename := "test_collect.gob.gz"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 50*time.Millisecond, WithGobCollectorGzip(gzip.BestSpeed))
	if err != nil {
		t.Fatalf("Failed to create compressed gob collector: %v", err)
	}

	data := []testCSVData{
		{ID: 1, Message: "test1", Value: 1.23},
		{ID: 2, Message: "test2", Value: 4.56},
	}
	for _, item := range data {
		collector.Collect(item)
	}
	collector.Close()

	got := readGobRecords[testCSVData](t, filename, true)
	if len(got) != len(data) {
		t.Fatalf("Expected %d records, got %d", len(data), len(got))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Errorf("Expected record %d to be %+v, got %+v", i, data[i], got[i])
		}
	}
}

func TestGobCollector_GzipNoCompressionCollect(t *testing.T) {
	filename := "test_collect_no_compression.gob.gz"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 50*time.Millisecond, WithGobCollectorGzip(gzip.NoCompression))
	if err != nil {
		t.Fatalf("Failed to create uncompressed gzip gob collector: %v", err)
	}

	data := testCSVData{ID: 1, Message: "test1", Value: 1.23}
	collector.Collect(data)
	if err := collector.CloseAndErr(); err != nil {
		t.Fatalf("Gob collector returned error: %v", err)
	}

	got := readGobRecords[testCSVData](t, filename, true)
	if len(got) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(got))
	}
	if got[0] != data {
		t.Errorf("Expected record to be %+v, got %+v", data, got[0])
	}
}

func TestGobCollector_ConcurrentCollect(t *testing.T) {
	filename := "test_concurrent.gob"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 50*time.Millisecond, WithGobCollectorBufferSize(16))
	if err != nil {
		t.Fatalf("Failed to create gob collector: %v", err)
	}

	const numGoroutines = 10
	const itemsPerGoroutine = 9

	var wg sync.WaitGroup
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range itemsPerGoroutine {
				collector.Collect(testCSVData{
					ID:      i*itemsPerGoroutine + j,
					Message: "",
					Value:   float64(i + j),
				})
			}
		})
	}
	wg.Wait()
	collector.Close()

	got := readGobRecords[testCSVData](t, filename, false)
	if len(got) != numGoroutines*itemsPerGoroutine {
		t.Fatalf("Expected %d records, got %d", numGoroutines*itemsPerGoroutine, len(got))
	}

	foundIDs := make(map[int]bool)
	for _, item := range got {
		foundIDs[item.ID] = true
	}
	for i := range numGoroutines * itemsPerGoroutine {
		if !foundIDs[i] {
			t.Errorf("Missing ID %d in gob output", i)
		}
	}
}

func TestGobCollector_MultipleClose(t *testing.T) {
	filename := "test_multiple_close.gob"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create gob collector: %v", err)
	}

	collector.Close()
	collector.Close()
	collector.Close()
}

func TestGobCollector_CollectAfterCloseDoesNotPanic(t *testing.T) {
	filename := "test_collect_after_close.gob"
	defer os.Remove(filename)

	collector, err := NewGobCollector[testCSVData](filename, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create gob collector: %v", err)
	}

	collector.Close()
	collector.Collect(testCSVData{ID: 1, Message: "late", Value: 1.0})

	if err := collector.Err(); err == nil {
		t.Fatal("Expected error after collecting on a closed collector, got nil")
	}
}

func readGobRecords[R any](t *testing.T, filename string, compressed bool) []R {
	t.Helper()

	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Failed to open gob file: %v", err)
	}
	defer file.Close()

	var reader io.Reader = file
	if compressed {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			t.Fatalf("Failed to open gzip reader: %v", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	decoder := gob.NewDecoder(reader)
	var records []R
	for {
		var record R
		err := decoder.Decode(&record)
		if err == io.EOF {
			return records
		}
		if err != nil {
			t.Fatalf("Failed to decode gob record: %v", err)
		}
		records = append(records, record)
	}
}
