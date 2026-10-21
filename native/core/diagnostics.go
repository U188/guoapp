package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const diagnosticLogBytes = 5 * 1024 * 1024

const diagnosticLogBackups = 2

type diagnosticEvent struct {
	HTTPStatus         int       `json:"httpStatus,omitempty"`
	Client             string    `json:"client,omitempty"`
	Protocol           string    `json:"protocol,omitempty"`
	CFRay              string    `json:"cfRay,omitempty"`
	ResponseType       string    `json:"responseType,omitempty"`
	RequestedQuality   int       `json:"requestedQuality,omitempty"`
	Time               time.Time `json:"time"`
	Level              string    `json:"level"`
	Event              string    `json:"event"`
	Source             string    `json:"source,omitempty"`
	DramaID            string    `json:"dramaId,omitempty"`
	DramaTitle         string    `json:"dramaTitle,omitempty"`
	Episode            int       `json:"episode,omitempty"`
	Run                uint64    `json:"run,omitempty"`
	StartSeconds       float64   `json:"startSeconds,omitempty"`
	Host               string    `json:"host,omitempty"`
	OffsetBytes        int64     `json:"offsetBytes,omitempty"`
	TotalBytes         int64     `json:"totalBytes,omitempty"`
	Attempt            int       `json:"attempt,omitempty"`
	Quality            int       `json:"quality,omitempty"`
	AvailableQualities []int     `json:"availableQualities,omitempty"`
	Message            string    `json:"message"`
}

type diagnosticLog struct {
	mu          sync.Mutex
	path        string
	limit       int64
	warningOnce sync.Once
}

func newDiagnosticLog(directory string) *diagnosticLog {
	return &diagnosticLog{path: filepath.Join(directory, "logs", "app.log"), limit: diagnosticLogBytes}
}

func (log *diagnosticLog) write(event diagnosticEvent) error {
	event.Time = time.Now()
	if event.Level == "" {
		event.Level = "error"
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	log.mu.Lock()
	defer log.mu.Unlock()
	if int64(len(data)) > log.limit {
		return errors.New("单条诊断日志过大")
	}
	if err := os.MkdirAll(filepath.Dir(log.path), 0700); err != nil {
		return err
	}
	info, err := os.Stat(log.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info != nil && info.Size()+int64(len(data)) > log.limit {
		if err := log.rotate(); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(log.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func (log *diagnosticLog) rotate() error {
	oldest := log.path + "." + strconv.Itoa(diagnosticLogBackups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for index := diagnosticLogBackups; index > 0; index-- {
		previous := log.path
		if index > 1 {
			previous += "." + strconv.Itoa(index-1)
		}
		if err := os.Rename(previous, log.path+"."+strconv.Itoa(index)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func redactConfiguredString(cfg *Config, value string) string {
	for _, secret := range []string{cfg.Token, cfg.AESKeyHex, cfg.InterfaceKey, cfg.ParamKey, cfg.ParamIV} {
		if secret = strings.TrimSpace(secret); secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return redactErrorString(value)
}

func (downloader *Downloader) recordDiagnostic(event diagnosticEvent) {
	if downloader == nil || downloader.diagnostics == nil {
		return
	}
	event.Message = truncate(redactConfiguredString(&downloader.cfg, event.Message), 4096)
	event.DramaTitle = truncate(redactConfiguredString(&downloader.cfg, event.DramaTitle), 256)
	if err := downloader.diagnostics.write(event); err != nil {
		downloader.diagnostics.warningOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "诊断日志保存失败：%v\n", publicError(err))
		})
	}
}

func (log *diagnosticLog) tail(limit int) []string {
	if log == nil || limit <= 0 || log.path == "" {
		return nil
	}
	if limit > 500 {
		limit = 500
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	files := []string{log.path + ".1", log.path}
	var lines []string
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				lines = append(lines, line)
			}
		}
		_ = file.Close()
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, diagnosticLineText(line))
	}
	return out
}

func diagnosticLineText(line string) string {
	var event diagnosticEvent
	if json.Unmarshal([]byte(line), &event) != nil {
		return truncate(line, 512)
	}
	var builder strings.Builder
	builder.WriteString(event.Time.Format("01-02 15:04:05"))
	builder.WriteString(" ")
	builder.WriteString(firstNonEmpty(event.Level, "info"))
	builder.WriteString(" ")
	builder.WriteString(event.Event)
	for _, field := range []string{event.Source, event.DramaID} {
		if field != "" {
			builder.WriteString(" ")
			builder.WriteString(field)
		}
	}
	if event.Episode > 0 {
		builder.WriteString(" #" + strconv.Itoa(event.Episode))
	}
	if event.HTTPStatus > 0 {
		builder.WriteString(" http=" + strconv.Itoa(event.HTTPStatus))
	}
	if event.ResponseType != "" {
		builder.WriteString(" type=" + event.ResponseType)
	}
	if event.TotalBytes > 0 {
		builder.WriteString(" bytes=" + strconv.FormatInt(event.TotalBytes, 10))
	}
	if event.Quality > 0 {
		builder.WriteString(" q=" + strconv.Itoa(event.Quality))
	}
	if event.Host != "" {
		builder.WriteString(" host=" + event.Host)
	}
	if event.Attempt > 1 {
		builder.WriteString(" attempt=" + strconv.Itoa(event.Attempt))
	}
	if event.Message != "" {
		builder.WriteString(" " + event.Message)
	}
	return truncate(builder.String(), 1024)
}

func (downloader *Downloader) diagnosticLogTail(limit int) []string {
	if downloader == nil || downloader.diagnostics == nil {
		return nil
	}
	return downloader.diagnostics.tail(limit)
}

func diagnosticKeys(value map[string]any, limit int) string {
	if len(value) == 0 {
		return ""
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	return strings.Join(keys, ",")
}

func diagnosticBodyPreview(content []byte) string {
	text := strings.Join(strings.Fields(string(content)), " ")
	if text == "" {
		return "响应体为空"
	}
	return truncate(redactErrorString(text), 240)
}
