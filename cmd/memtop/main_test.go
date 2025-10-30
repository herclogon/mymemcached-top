package main

import (
	"bufio"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestCalculateRates(t *testing.T) {
	prev := &statsSnapshot{
		Timestamp: time.Now(),
		Values: map[string]float64{
			"cmd_get":   100,
			"evictions": 6,
		},
	}
	curr := &statsSnapshot{
		Timestamp: prev.Timestamp.Add(2 * time.Second),
		Values: map[string]float64{
			"cmd_get":   140,
			"evictions": 4, // values can drop (server restart); rate should not go negative
		},
	}

	rates := calculateRates(curr, prev)

	if got, want := rates["cmd_get"], 20.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("cmd_get rate mismatch: got %.2f, want %.2f", got, want)
	}
	if got := rates["evictions"]; got != 0 {
		t.Fatalf("evictions rate should clamp to zero, got %.2f", got)
	}
	if _, ok := rates["missing"]; ok {
		t.Fatalf("unexpected rate entry produced for missing key")
	}
}

func TestRateValueNilMap(t *testing.T) {
	if got := rateValue(nil, "cmd_get"); got != 0 {
		t.Fatalf("rateValue with nil map: got %.2f, want 0", got)
	}
	rates := map[string]float64{"bytes_read": 42.5}
	if got := rateValue(rates, "bytes_read"); got != 42.5 {
		t.Fatalf("rateValue returned %.2f, want 42.5", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := map[string]struct {
		value float64
		want  string
	}{
		"bytes":         {value: 512, want: "512 B"},
		"kilobytes":     {value: 1024, want: "1.0 KB"},
		"megabytes":     {value: 1.5 * 1024 * 1024, want: "1.5 MB"},
		"negativeClamp": {value: -10, want: "0 B"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := formatBytes(tc.value); got != tc.want {
				t.Fatalf("formatBytes(%.2f) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestFormatBytesRate(t *testing.T) {
	if got, want := formatBytesRate(2048), "2.0 KB/s"; got != want {
		t.Fatalf("formatBytesRate mismatch: got %q, want %q", got, want)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name   string
		secs   float64
		expect string
	}{
		{name: "zero", secs: 0, expect: "0s"},
		{name: "hours", secs: 3661, expect: "01h 01m 01s"},
		{name: "days", secs: 90061, expect: "1d 01h 01m 01s"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatUptime(tc.secs); got != tc.expect {
				t.Fatalf("formatUptime(%v) = %q, want %q", tc.secs, got, tc.expect)
			}
		})
	}
}

func TestBoolToWord(t *testing.T) {
	if got, want := boolToWord(true), "yes"; got != want {
		t.Fatalf("boolToWord(true) = %q, want %q", got, want)
	}
	if got, want := boolToWord(false), "no"; got != want {
		t.Fatalf("boolToWord(false) = %q, want %q", got, want)
	}
}

func TestFetchStatsParsesValues(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- fmt.Errorf("accept: %w", err)
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- fmt.Errorf("read command: %w", err)
			return
		}
		if line != "stats\r\n" {
			errCh <- fmt.Errorf("unexpected command %q", line)
			return
		}

		fmt.Fprint(conn, "STAT cmd_get 42\r\n")
		fmt.Fprint(conn, "STAT version 1.6.9\r\n")
		fmt.Fprint(conn, "STAT evictions not_a_number\r\n")
		fmt.Fprint(conn, "END\r\n")
		errCh <- nil
	}()

	snapshot, err := fetchStats(ln.Addr().String())
	if err != nil {
		t.Fatalf("fetchStats returned error: %v", err)
	}
	if acceptErr := <-errCh; acceptErr != nil {
		t.Fatalf("server handling failed: %v", acceptErr)
	}

	if got := snapshot.Values["cmd_get"]; got != 42 {
		t.Fatalf("cmd_get parsed as %.0f, want 42", got)
	}
	if _, ok := snapshot.Values["evictions"]; ok {
		t.Fatalf("non-numeric stat evictions should not populate Values map")
	}
	if got := snapshot.Raw["version"]; got != "1.6.9" {
		t.Fatalf("version parsed as %q, want %q", got, "1.6.9")
	}
	if snapshot.Timestamp.IsZero() {
		t.Fatalf("timestamp should be populated")
	}
}

func TestDrawScreenRendersKeySections(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("simulation screen init failed: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 20)

	stats := &statsSnapshot{
		Timestamp: time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC),
		Values: map[string]float64{
			"uptime":                3661,
			"get_hits":              80,
			"get_misses":            20,
			"evictions":             2,
			"reclaimed":             1,
			"bytes":                 2048,
			"limit_maxbytes":        8192,
			"curr_connections":      5,
			"total_connections":     50,
			"reserved_fds":          1,
			"conn_yields":           2,
			"threads":               4,
			"curr_items":            100,
			"total_items":           200,
			"expired_unfetched":     3,
			"slab_global_page_pool": 4,
			"accepting_conns":       1,
		},
		Raw: map[string]string{
			"version": "1.6.0",
		},
	}

	rates := map[string]float64{
		"cmd_get":       4.5,
		"cmd_set":       2.0,
		"cmd_delete":    1.0,
		"incr_hits":     0.5,
		"incr_misses":   0.2,
		"decr_hits":     0.1,
		"touch_hits":    0.3,
		"touch_misses":  0.1,
		"bytes_read":    1024,
		"bytes_written": 2048,
	}

	drawScreen(screen, "127.0.0.1:11211", 2*time.Second, stats, rates, nil)

	cells, width, height := screen.GetContents()
	if height == 0 || width == 0 {
		t.Fatalf("screen contents not captured")
	}

	header := lineFromCells(cells, width, 0)
	if !strings.Contains(header, "mymemcache-top") {
		t.Fatalf("header line missing title, got %q", header)
	}
	timeLine := lineFromCells(cells, width, 2)
	if !strings.Contains(timeLine, "Uptime: 01h 01m 01s") {
		t.Fatalf("time line missing uptime, got %q", timeLine)
	}
	memoryLine := lineFromCells(cells, width, 5)
	if !strings.Contains(memoryLine, "Memory: 2.0 KB / 8.0 KB (25.0%)   Free: 6.0 KB") {
		t.Fatalf("memory line unexpected, got %q", memoryLine)
	}
	controls := lineFromCells(cells, width, height-1)
	if !strings.Contains(controls, "Controls: q to quit | r to reset rate baseline") {
		t.Fatalf("controls line missing help text, got %q", controls)
	}
}

func lineFromCells(cells []tcell.SimCell, width, row int) string {
	start := row * width
	end := start + width
	if start < 0 || end > len(cells) {
		return ""
	}
	var b strings.Builder
	for _, cell := range cells[start:end] {
		if len(cell.Runes) == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(cell.Runes[0])
	}
	return strings.TrimRight(b.String(), " ")
}
