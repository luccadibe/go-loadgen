package go_loadgen

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type benchmarkCollectorRecord struct {
	ID        int64
	Timestamp int64
	LatencyNS int64
	Status    int
	Endpoint  string
	Method    string
	Error     string
	BytesIn   int64
	BytesOut  int64
	OK        bool
}

type benchmarkResultCollector interface {
	Collect(benchmarkCollectorRecord)
	Close()
}

func (r benchmarkCollectorRecord) CSVHeaders() []string {
	return []string{"id", "timestamp", "latency_ns", "status", "endpoint", "method", "error", "bytes_in", "bytes_out", "ok"}
}

func (r benchmarkCollectorRecord) CSVRecord() []string {
	return []string{
		strconv.FormatInt(r.ID, 10),
		strconv.FormatInt(r.Timestamp, 10),
		strconv.FormatInt(r.LatencyNS, 10),
		strconv.Itoa(r.Status),
		r.Endpoint,
		r.Method,
		r.Error,
		strconv.FormatInt(r.BytesIn, 10),
		strconv.FormatInt(r.BytesOut, 10),
		strconv.FormatBool(r.OK),
	}
}

func BenchmarkCollectorsSequential(b *testing.B) {
	benchCollectorSequential(b, "csv", func(path string) (benchmarkResultCollector, error) {
		return NewCSVCollector[benchmarkCollectorRecord](path, time.Hour)
	})
	benchCollectorSequential(b, "gob", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536))
	})
	benchCollectorSequential(b, "gob_gzip_best_speed", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536), WithGobCollectorGzip(gzip.BestSpeed))
	})
	benchCollectorSequential(b, "gob_gzip_best_compression", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536), WithGobCollectorGzip(gzip.BestCompression))
	})
}

func BenchmarkCollectorsParallel(b *testing.B) {
	benchCollectorParallel(b, "csv", func(path string) (benchmarkResultCollector, error) {
		return NewCSVCollector[benchmarkCollectorRecord](path, time.Hour)
	})
	benchCollectorParallel(b, "gob", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536))
	})
	benchCollectorParallel(b, "gob_gzip_best_speed", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536), WithGobCollectorGzip(gzip.BestSpeed))
	})
	benchCollectorParallel(b, "gob_gzip_best_compression", func(path string) (benchmarkResultCollector, error) {
		return NewGobCollector[benchmarkCollectorRecord](path, time.Hour, WithGobCollectorBufferSize(65536), WithGobCollectorGzip(gzip.BestCompression))
	})
}

func TestCollectorStressCSVVsGob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping collector stress test in short mode")
	}

	const records = 100_000
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "results.csv")
	gobPath := filepath.Join(dir, "results.gob")
	gzipPath := filepath.Join(dir, "results.gob.gz")

	csvCollector, err := NewCSVCollector[benchmarkCollectorRecord](csvPath, time.Hour)
	if err != nil {
		t.Fatalf("create CSV collector: %v", err)
	}
	gobCollector, err := NewGobCollector[benchmarkCollectorRecord](gobPath, time.Hour, WithGobCollectorBufferSize(65536))
	if err != nil {
		t.Fatalf("create gob collector: %v", err)
	}
	gzipCollector, err := NewGobCollector[benchmarkCollectorRecord](gzipPath, time.Hour, WithGobCollectorBufferSize(65536), WithGobCollectorGzip(gzip.BestSpeed))
	if err != nil {
		t.Fatalf("create gzip gob collector: %v", err)
	}

	start := time.Now()
	for i := 0; i < records; i++ {
		csvCollector.Collect(benchmarkRecord(int64(i)))
	}
	csvCollector.Close()
	csvElapsed := time.Since(start)

	start = time.Now()
	for i := 0; i < records; i++ {
		gobCollector.Collect(benchmarkRecord(int64(i)))
	}
	if err := gobCollector.CloseAndErr(); err != nil {
		t.Fatalf("close gob collector: %v", err)
	}
	gobElapsed := time.Since(start)

	start = time.Now()
	for i := 0; i < records; i++ {
		gzipCollector.Collect(benchmarkRecord(int64(i)))
	}
	if err := gzipCollector.CloseAndErr(); err != nil {
		t.Fatalf("close gzip gob collector: %v", err)
	}
	gzipElapsed := time.Since(start)
	t.Logf("wrote %d records: csv=%s gob=%s gob.gz=%s", records, csvElapsed, gobElapsed, gzipElapsed)

	csvSize := fileSize(t, csvPath)
	gobSize := fileSize(t, gobPath)
	gzipSize := fileSize(t, gzipPath)
	t.Logf("csv=%d bytes gob=%d bytes gob.gz=%d bytes", csvSize, gobSize, gzipSize)
	t.Logf("gob/csv=%.2f gob.gz/csv=%.2f", float64(gobSize)/float64(csvSize), float64(gzipSize)/float64(csvSize))

	if got := countGobRecords[benchmarkCollectorRecord](t, gobPath, false); got != records {
		t.Fatalf("decoded %d gob records, want %d", got, records)
	}
	if got := countGobRecords[benchmarkCollectorRecord](t, gzipPath, true); got != records {
		t.Fatalf("decoded %d gzip gob records, want %d", got, records)
	}
	if gzipSize >= csvSize {
		t.Logf("compressed gob is not smaller than CSV for this fixture: gzip=%d csv=%d", gzipSize, csvSize)
	}
}

func benchCollectorSequential(b *testing.B, name string, newCollector func(string) (benchmarkResultCollector, error)) {
	b.Run(name, func(b *testing.B) {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("%s.out", name))
		collector, err := newCollector(path)
		if err != nil {
			b.Fatalf("create collector: %v", err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collector.Collect(benchmarkRecord(int64(i)))
		}
		collector.Close()
		b.StopTimer()
		if errCollector, ok := collector.(interface{ Err() error }); ok {
			if err := errCollector.Err(); err != nil {
				b.Fatalf("collector error: %v", err)
			}
		}
		reportCollectorOutputMetrics(b, path, b.N)
	})
}

func benchCollectorParallel(b *testing.B, name string, newCollector func(string) (benchmarkResultCollector, error)) {
	b.Run(name, func(b *testing.B) {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("%s.out", name))
		collector, err := newCollector(path)
		if err != nil {
			b.Fatalf("create collector: %v", err)
		}

		var id atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				collector.Collect(benchmarkRecord(id.Add(1)))
			}
		})
		collector.Close()
		b.StopTimer()
		if errCollector, ok := collector.(interface{ Err() error }); ok {
			if err := errCollector.Err(); err != nil {
				b.Fatalf("collector error: %v", err)
			}
		}
		reportCollectorOutputMetrics(b, path, b.N)
	})
}

func benchmarkRecord(id int64) benchmarkCollectorRecord {
	status := 200
	errText := ""
	if id%97 == 0 {
		status = 503
		errText = "upstream unavailable"
	}

	return benchmarkCollectorRecord{
		ID:        id,
		Timestamp: 1_720_000_000_000_000_000 + id,
		LatencyNS: 500_000 + (id % 25_000_000),
		Status:    status,
		Endpoint:  "/v1/accounts/{account_id}/transactions/search",
		Method:    "POST",
		Error:     errText,
		BytesIn:   512 + id%2048,
		BytesOut:  4096 + id%65536,
		OK:        status < 500,
	}
}

func reportCollectorOutputMetrics(b *testing.B, path string, records int) {
	b.Helper()
	if records == 0 {
		return
	}
	size := fileSize(b, path)
	b.ReportMetric(float64(size), "output_bytes")
	b.ReportMetric(float64(size)/float64(records), "bytes/record")
}

func fileSize(tb testing.TB, path string) int64 {
	tb.Helper()
	info, err := os.Stat(path)
	if err != nil {
		tb.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

func countGobRecords[R any](t *testing.T, path string, compressed bool) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open gob output: %v", err)
	}
	defer file.Close()

	var reader io.Reader = file
	if compressed {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			t.Fatalf("open gzip output: %v", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	decoder := gob.NewDecoder(reader)
	count := 0
	for {
		var record R
		err := decoder.Decode(&record)
		if err == io.EOF {
			return count
		}
		if err != nil {
			t.Fatalf("decode gob record %d: %v", count, err)
		}
		count++
	}
}
