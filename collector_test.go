package go_loadgen

import (
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
