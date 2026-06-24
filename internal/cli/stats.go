// Package cli implements command-line subcommands for apiproxy, such as
// inspecting recorded statistics without starting the HTTP server.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/wangyong/apiproxy/internal/storage"
)

// StatsOptions configures the stats subcommand.
type StatsOptions struct {
	Window   time.Duration
	DBPath   string
	Provider string
	Model    string
	Route    string
	Interval string // "minute", "hour", "day", or "" for auto
	AsJSON   bool
	Lang     string // "en" (default) or "zh"
}

// DefaultWindow is the default lookback window for the stats subcommand.
const DefaultWindow = 10 * time.Minute

// RegisterStatsFlags wires stats options onto an existing FlagSet.
func RegisterStatsFlags(fs *flag.FlagSet, opts *StatsOptions) {
	fs.DurationVar(&opts.Window, "window", DefaultWindow, "lookback window (e.g. 10m, 1h, 24h)")
	fs.StringVar(&opts.DBPath, "db", "", "path to SQLite database (overrides -config)")
	fs.StringVar(&opts.Provider, "provider", "", "filter by provider")
	fs.StringVar(&opts.Model, "model", "", "filter by model")
	fs.StringVar(&opts.Route, "route", "", "filter by route")
	fs.StringVar(&opts.Interval, "interval", "", "timeseries bucket: minute, hour, day (default auto from window)")
	fs.BoolVar(&opts.AsJSON, "json", false, "emit raw JSON instead of tables")
	fs.StringVar(&opts.Lang, "lang", "", "output language: 'en' (default) or 'zh'")
}

// translateErrMsg converts inline error messages to the target language.
func translateErrMsg(lang, msg string) string {
	if lang != "zh" {
		return msg
	}
	switch {
	case strings.HasPrefix(msg, "invalid -window"):
		return "无效的 -window: 必须为正数"
	case strings.HasPrefix(msg, "missing -db"):
		return "缺少 -db 或带 storage.path 的 -config"
	}
	return msg
}

// PrintStats opens the SQLite store at opts.DBPath and prints aggregate
// statistics for the last opts.Window to out.
//
// On success it returns nil; the caller is responsible for nothing — the
// store is opened and closed inside this function.
func PrintStats(ctx context.Context, out io.Writer, opts StatsOptions) error {
	lang := opts.Lang
	if lang == "" {
		lang = "en"
	}

	if opts.Window <= 0 {
		return fmt.Errorf("invalid -window: must be positive")
	}
	if opts.DBPath == "" {
		return fmt.Errorf("missing -db or -config with storage.path")
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		return fmt.Errorf("stat db: %w", err)
	}

	store, err := storage.Open(opts.DBPath, 0)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	f := storage.QueryFilter{
		Start:    time.Now().Add(-opts.Window),
		Provider: opts.Provider,
		Model:    opts.Model,
		Route:    opts.Route,
	}

	interval := opts.Interval
	if interval == "" {
		interval = autoInterval(opts.Window)
	}

	summaries, err := store.ModelSummaries(ctx, f)
	if err != nil {
		return fmt.Errorf("model summaries: %w", err)
	}
	pcts, err := store.LatencyPercentiles(ctx, f)
	if err != nil {
		return fmt.Errorf("percentiles: %w", err)
	}
	buckets, err := store.SpeedByPromptBucket(ctx, f, nil)
	if err != nil {
		return fmt.Errorf("buckets: %w", err)
	}
	ts, err := store.TimeSeries(ctx, f, interval)
	if err != nil {
		return fmt.Errorf("timeseries: %w", err)
	}

	if opts.AsJSON {
		return writeJSON(out, summaries, pcts, buckets, ts, opts.Window, interval)
	}

	writeSummary(out, summaries, pcts, opts.Window, lang)
	writeBuckets(out, buckets, lang)
	writeTimeseries(out, ts, lang)
	return nil
}

func writeSummary(out io.Writer, summaries []storage.ModelSummary, pcts []storage.PercentileRow, window time.Duration, lang string) {
	pctMap := make(map[string]storage.PercentileRow, len(pcts))
	for _, p := range pcts {
		pctMap[p.Provider+"\x00"+p.Model] = p
	}

	fmt.Fprintln(out)
	if lang == "zh" {
		fmt.Fprintf(out, "最近 %s 的统计\n", humanWindow(window, lang))
	} else {
		fmt.Fprintf(out, "Stats for last %s\n", humanWindow(window, lang))
	}
	fmt.Fprintln(out)
	if len(summaries) == 0 {
		if lang == "zh" {
			fmt.Fprintln(out, "  (没有数据)")
		} else {
			fmt.Fprintln(out, "  (no data)")
		}
		return
	}

	var header []string
	if lang == "zh" {
		header = []string{"Provider", "Model", "Route", "请求", "错误", "成功率", "平均延迟", "P50", "P95", "P99", "TG tok/s", "Prompt", "Completion", "Stream"}
	} else {
		header = []string{"Provider", "Model", "Route", "Requests", "Errors", "Success %", "Avg", "P50", "P95", "P99", "TG tok/s", "Prompt", "Completion", "Stream"}
	}
	rows := make([][]string, 0, len(summaries))
	for _, m := range summaries {
		row := []string{
			m.Provider, m.Model, m.Route,
			fmt.Sprintf("%d", m.Requests),
			fmt.Sprintf("%d", m.Errors),
			fmt.Sprintf("%.1f%%", m.SuccessRate*100),
			fmt.Sprintf("%.0fms", m.AvgLatencyMs),
		}
		if p, ok := pctMap[m.Provider+"\x00"+m.Model]; ok {
			row = append(row,
				fmt.Sprintf("%.0fms", p.P50LatencyMs),
				fmt.Sprintf("%.0fms", p.P95LatencyMs),
				fmt.Sprintf("%.0fms", p.P99LatencyMs),
			)
		} else {
			row = append(row, "-", "-", "-")
		}
		row = append(row,
			fmt.Sprintf("%.1f", m.TokensPerSec),
			fmt.Sprintf("%d", m.PromptTokens),
			fmt.Sprintf("%d", m.CompletionTokens),
			fmt.Sprintf("%d", m.StreamRequests),
		)
		rows = append(rows, row)
	}
	writeTable(out, header, rows)
}

func writeBuckets(out io.Writer, rows []storage.BucketRow, lang string) {
	fmt.Fprintln(out)
	if lang == "zh" {
		fmt.Fprintln(out, "按上下文长度分桶 (PP/TG 速度)")
	} else {
		fmt.Fprintln(out, "By context length bucket (PP/TG speed)")
	}
	fmt.Fprintln(out)
	if len(rows) == 0 {
		if lang == "zh" {
			fmt.Fprintln(out, "  (没有数据)")
		} else {
			fmt.Fprintln(out, "  (no data)")
		}
		return
	}

	var header []string
	if lang == "zh" {
		header = []string{"Provider", "Model", "桶", "请求", "Prompt", "Completion", "平均延迟", "PP tok/s", "TG tok/s"}
	} else {
		header = []string{"Provider", "Model", "Bucket", "Requests", "Prompt", "Completion", "Avg Latency", "PP tok/s", "TG tok/s"}
	}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		data = append(data, []string{
			r.Provider, r.Model, r.Bucket,
			fmt.Sprintf("%d", r.Requests),
			fmt.Sprintf("%d", r.PromptTokens),
			fmt.Sprintf("%d", r.CompletionTokens),
			fmt.Sprintf("%.0fms", r.AvgLatencyMs),
			fmt.Sprintf("%.1f", r.PPRate),
			fmt.Sprintf("%.1f", r.TGRate),
		})
	}
	writeTable(out, header, data)
}

func writeJSON(out io.Writer, summaries []storage.ModelSummary, pcts []storage.PercentileRow, buckets []storage.BucketRow, ts []storage.TimeSeriesPoint, window time.Duration, interval string) error {
	pctMap := make(map[string]storage.PercentileRow, len(pcts))
	for _, p := range pcts {
		pctMap[p.Provider+"\x00"+p.Model] = p
	}
	type summaryWithPcts struct {
		storage.ModelSummary
		P50 float64 `json:"p50_latency_ms"`
		P95 float64 `json:"p95_latency_ms"`
		P99 float64 `json:"p99_latency_ms"`
	}
	combined := make([]summaryWithPcts, 0, len(summaries))
	for _, m := range summaries {
		row := summaryWithPcts{ModelSummary: m}
		if p, ok := pctMap[m.Provider+"\x00"+m.Model]; ok {
			row.P50 = p.P50LatencyMs
			row.P95 = p.P95LatencyMs
			row.P99 = p.P99LatencyMs
		}
		combined = append(combined, row)
	}

	payload := map[string]any{
		"window_minutes": int(window / time.Minute),
		"interval":       interval,
		"summaries":      combined,
		"buckets":        buckets,
		"timeseries":     ts,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// writeTable prints an aligned ASCII table. Numbers are right-aligned in
// the cell, everything else is left-aligned.
func writeTable(out io.Writer, header []string, rows [][]string) {
	right := make([]bool, len(header))
	for i, h := range header {
		switch h {
		case "请求", "错误", "成功率", "平均", "平均延迟", "P50", "P95", "P99", "TPS",
			"Requests", "Errors", "Success %", "Avg", "Avg Latency", "TG tok/s",
			"Prompt", "Completion", "Compl", "Stream", "桶", "Bucket", "PP tok/s":
			right[i] = true
		}
	}
	_ = right

	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = displayWidth(h)
	}
	for _, r := range rows {
		for i, cell := range r {
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var sb strings.Builder
	for i, h := range header {
		writeCell(&sb, h, widths[i], right[i])
		if i < len(header)-1 {
			sb.WriteString("  ")
		}
	}
	fmt.Fprintln(out, sb.String())

	sb.Reset()
	for i := range header {
		writeCell(&sb, strings.Repeat("-", widths[i]), widths[i], false)
		if i < len(header)-1 {
			sb.WriteString("  ")
		}
	}
	fmt.Fprintln(out, sb.String())

	for _, r := range rows {
		sb.Reset()
		for i, cell := range r {
			writeCell(&sb, cell, widths[i], right[i])
			if i < len(r)-1 {
				sb.WriteString("  ")
			}
		}
		fmt.Fprintln(out, sb.String())
	}
}

// writeCell writes a single cell, right-aligning if right is true. CJK
// characters take two columns of display width, which we pad for.
func writeCell(sb *strings.Builder, s string, width int, right bool) {
	w := displayWidth(s)
	pad := width - w
	if pad < 0 {
		pad = 0
	}
	if right {
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(s)
		return
	}
	sb.WriteString(s)
	sb.WriteString(strings.Repeat(" ", pad))
}

// displayWidth returns the number of monospace columns a string occupies,
// counting CJK characters as 2.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F, // CJK radicals / Kangxi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK Compat Ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK Compat Forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x2FFFD,
		r >= 0x30000 && r <= 0x3FFFD:
		return 2
	default:
		return 1
	}
}

func humanWindow(d time.Duration, lang string) string {
	if d >= time.Hour && d%time.Hour == 0 {
		if lang == "zh" {
			return fmt.Sprintf("%d 小时", int(d/time.Hour))
		}
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		if lang == "zh" {
			return fmt.Sprintf("%d 分钟", int(d/time.Minute))
		}
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	}
	return d.String()
}

func writeTimeseries(out io.Writer, rows []storage.TimeSeriesPoint, lang string) {
	fmt.Fprintln(out)
	if lang == "zh" {
		fmt.Fprintln(out, "时间序列趋势")
	} else {
		fmt.Fprintln(out, "Time series trend")
	}
	fmt.Fprintln(out)
	if len(rows) == 0 {
		if lang == "zh" {
			fmt.Fprintln(out, "  (没有数据)")
		} else {
			fmt.Fprintln(out, "  (no data)")
		}
		return
	}

	var header []string
	if lang == "zh" {
		header = []string{"时间", "Provider", "Model", "请求", "错误", "平均延迟", "Prompt", "Completion", "TG tok/s"}
	} else {
		header = []string{"Time", "Provider", "Model", "Requests", "Errors", "Avg Latency", "Prompt", "Completion", "TG tok/s"}
	}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		data = append(data, []string{
			r.Ts,
			r.Provider,
			r.Model,
			fmt.Sprintf("%d", r.Requests),
			fmt.Sprintf("%d", r.Errors),
			fmt.Sprintf("%.0fms", r.AvgLatencyMs),
			fmt.Sprintf("%d", r.PromptTokens),
			fmt.Sprintf("%d", r.CompletionTokens),
			fmt.Sprintf("%.1f", r.TokensPerSec),
		})
	}
	writeTable(out, header, data)
}

// autoInterval picks a reasonable timeseries bucket size based on the window.
func autoInterval(window time.Duration) string {
	if window <= 2*time.Hour {
		return "minute"
	}
	if window <= 48*time.Hour {
		return "hour"
	}
	return "day"
}
