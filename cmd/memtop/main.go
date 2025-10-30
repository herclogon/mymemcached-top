package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

type statsSnapshot struct {
	Timestamp time.Time
	Values    map[string]float64
	Raw       map[string]string
}

const defaultTimeout = 2 * time.Second

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] [host [port]]\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "\nOptions:")
		flag.PrintDefaults()
	}

	host := flag.String("host", "127.0.0.1", "memcached host (overridable by first positional arg)")
	port := flag.Int("port", 11211, "memcached port (overridable by second positional arg)")
	interval := flag.Duration("interval", 2*time.Second, "refresh interval")
	flag.Parse()

	hostVal := *host
	portVal := *port
	args := flag.Args()
	if len(args) > 0 {
		hostVal = args[0]
	}
	if len(args) > 1 {
		p, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid port %q: %v\n", args[1], err)
			os.Exit(2)
		}
		portVal = p
	}

	addr := fmt.Sprintf("%s:%d", hostVal, portVal)

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create screen: %v\n", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init screen: %v\n", err)
		os.Exit(1)
	}
	defer screen.Fini()

	screen.Clear()
	screen.HideCursor()

	eventCh := make(chan tcell.Event, 8)
	go func() {
		for {
			event := screen.PollEvent()
			if event == nil {
				close(eventCh)
				return
			}
			eventCh <- event
		}
	}()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	var (
		currentStats *statsSnapshot
		prevStats    *statsSnapshot
		rates        map[string]float64
		lastErr      error
	)

	drawScreen(screen, addr, *interval, currentStats, rates, lastErr)

loop:
	for {
		select {
		case <-ticker.C:
			stats, err := fetchStats(addr)
			if err != nil {
				lastErr = err
			} else {
				lastErr = nil
				if prevStats != nil {
					rates = calculateRates(stats, prevStats)
				} else {
					rates = make(map[string]float64)
				}
				prevStats = stats
				currentStats = stats
			}
			drawScreen(screen, addr, *interval, currentStats, rates, lastErr)
		case ev, ok := <-eventCh:
			if !ok {
				break loop
			}
			switch evt := ev.(type) {
			case *tcell.EventKey:
				switch {
				case evt.Key() == tcell.KeyEscape, evt.Key() == tcell.KeyCtrlC, evt.Rune() == 'q', evt.Rune() == 'Q':
					break loop
				case evt.Rune() == 'r' || evt.Rune() == 'R':
					prevStats = nil
					rates = make(map[string]float64)
					drawScreen(screen, addr, *interval, currentStats, rates, lastErr)
				}
			case *tcell.EventResize:
				screen.Sync()
				drawScreen(screen, addr, *interval, currentStats, rates, lastErr)
			}
		}
	}
}

func fetchStats(addr string) (*statsSnapshot, error) {
	conn, err := net.DialTimeout("tcp", addr, defaultTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return nil, err
	}

	if _, err := fmt.Fprint(conn, "stats\r\n"); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	values := make(map[string]float64)
	raw := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "END" {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "STAT" {
			continue
		}
		key := fields[1]
		value := strings.Join(fields[2:], " ")
		raw[key] = value
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			values[key] = number
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &statsSnapshot{
		Timestamp: time.Now(),
		Values:    values,
		Raw:       raw,
	}, nil
}

func calculateRates(curr, prev *statsSnapshot) map[string]float64 {
	result := make(map[string]float64)
	if curr == nil || prev == nil {
		return result
	}
	elapsed := curr.Timestamp.Sub(prev.Timestamp).Seconds()
	if elapsed <= 0 {
		return result
	}
	for key, currentVal := range curr.Values {
		if prevVal, ok := prev.Values[key]; ok {
			diff := currentVal - prevVal
			if diff < 0 {
				diff = 0
			}
			result[key] = diff / elapsed
		}
	}
	return result
}

func drawScreen(screen tcell.Screen, addr string, interval time.Duration, stats *statsSnapshot, rates map[string]float64, err error) {
	screen.Clear()
	width, height := screen.Size()
	if height <= 0 || width <= 0 {
		screen.Show()
		return
	}

	baseStyle := tcell.StyleDefault
	highlightStyle := baseStyle.Bold(true)

	drawText(screen, 0, 0, highlightStyle, fmt.Sprintf("mymemcache-top  %s  (refresh %s)", addr, interval))

	line := 2

	if err != nil {
		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Error: %v", err))
		line += 2
	}

	if stats != nil {
		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Time: %s    Uptime: %s    Version: %s",
			stats.Timestamp.Format("2006-01-02 15:04:05"),
			formatUptime(stats.Values["uptime"]),
			stats.Raw["version"],
		))
		line++

		getHits := stats.Values["get_hits"]
		getMisses := stats.Values["get_misses"]
		totalGets := getHits + getMisses
		hitRatio := 0.0
		if totalGets > 0 {
			hitRatio = (getHits / totalGets) * 100
		}
		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Requests: hits %.0f  misses %.0f  hit ratio %.2f%%  evictions %.0f  reclaimed %.0f",
			getHits, getMisses, hitRatio, stats.Values["evictions"], stats.Values["reclaimed"]))
		line += 2

		bytesUsed := stats.Values["bytes"]
		maxBytes := stats.Values["limit_maxbytes"]
		memoryPercent := 0.0
		if maxBytes > 0 {
			memoryPercent = (bytesUsed / maxBytes) * 100
		}
		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Memory: %s / %s (%.1f%%)   Free: %s",
			formatBytes(bytesUsed), formatBytes(maxBytes), memoryPercent, formatBytes(maxBytes-bytesUsed)))
		line++

		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Connections: current %.0f  total %.0f  reserved %.0f  waiting %.0f  max simultaneous %.0f",
			stats.Values["curr_connections"],
			stats.Values["total_connections"],
			stats.Values["reserved_fds"],
			stats.Values["conn_yields"],
			stats.Values["threads"],
		))
		line++

		cmdGetRate := rateValue(rates, "cmd_get")
		cmdSetRate := rateValue(rates, "cmd_set")
		cmdDeleteRate := rateValue(rates, "cmd_delete")
		incrRate := rateValue(rates, "incr_hits") + rateValue(rates, "incr_misses")
		decrRate := rateValue(rates, "decr_hits") + rateValue(rates, "decr_misses")
		touchRate := rateValue(rates, "touch_hits") + rateValue(rates, "touch_misses")
		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Commands/s: get %.2f  set %.2f  delete %.2f  incr %.2f  decr %.2f  touch %.2f",
			cmdGetRate, cmdSetRate, cmdDeleteRate, incrRate, decrRate, touchRate))
		line++

		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Bandwidth/s: read %s  write %s",
			formatBytesRate(rateValue(rates, "bytes_read")),
			formatBytesRate(rateValue(rates, "bytes_written")),
		))
		line++

		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Items: current %.0f  total %.0f  expired %.0f",
			stats.Values["curr_items"],
			stats.Values["total_items"],
			stats.Values["expired_unfetched"],
		))
		line++

		drawText(screen, 0, line, baseStyle, fmt.Sprintf("Slabs: %.0f  Threads: %.0f  Accepting connections: %s",
			stats.Values["slab_global_page_pool"],
			stats.Values["threads"],
			boolToWord(stats.Values["accepting_conns"] == 1),
		))
		line++
	} else if err == nil {
		drawText(screen, 0, line, baseStyle, "Waiting for initial stats...")
		line++
	}

	if height > 2 {
		drawText(screen, 0, height-1, highlightStyle,
			"Controls: q to quit | r to reset rate baseline")
	}

	screen.Show()
}

func drawText(screen tcell.Screen, x, y int, style tcell.Style, text string) {
	if y < 0 {
		return
	}
	width, height := screen.Size()
	if y >= height {
		return
	}
	for i, r := range text {
		pos := x + i
		if pos >= width {
			break
		}
		screen.SetContent(pos, y, r, nil, style)
	}
}

func rateValue(rates map[string]float64, key string) float64 {
	if rates == nil {
		return 0
	}
	return rates[key]
}

func formatBytes(b float64) string {
	if b < 0 {
		b = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	idx := 0
	for b >= 1024 && idx < len(units)-1 {
		b /= 1024
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%.0f %s", b, units[idx])
	}
	return fmt.Sprintf("%.1f %s", b, units[idx])
}

func formatBytesRate(bps float64) string {
	return fmt.Sprintf("%s/s", formatBytes(bps))
}

func formatUptime(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	duration := time.Duration(seconds * float64(time.Second))
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	seconds = math.Round(duration.Seconds())

	if days > 0 {
		return fmt.Sprintf("%dd %02dh %02dm %02ds", days, hours, minutes, int(seconds))
	}
	return fmt.Sprintf("%02dh %02dm %02ds", hours, minutes, int(seconds))
}

func boolToWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
