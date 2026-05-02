package runtimelog

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/kataras/golog"
)

var (
	mu            sync.Mutex
	currentOutput io.Writer = os.Stdout
	logger                  = newLogger()
)

func newLogger() *golog.Logger {
	l := golog.New()
	l.SetOutput(currentOutput)
	l.SetTimeFormat("")
	l.SetLevel("debug")
	l.SetPrefix("[react-runtime] ")
	return l
}

func SetOutput(w io.Writer) io.Writer {
	mu.Lock()
	defer mu.Unlock()
	prev := currentOutput
	if w == nil {
		w = os.Stdout
	}
	currentOutput = w
	logger.SetOutput(w)
	return prev
}

func LogJSON(level string, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any)
	}
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = "info"
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		fallback := "[invalid-json-payload]"
		switch level {
		case "error":
			logger.Errorf("%s", fallback)
		case "warn", "warning":
			logger.Warnf("%s", fallback)
		case "debug":
			logger.Debugf("%s", fallback)
		default:
			logger.Infof("%s", fallback)
		}
		return
	}

	switch level {
	case "error":
		logger.Errorf("%s", string(rawPayload))
	case "warn", "warning":
		logger.Warnf("%s", string(rawPayload))
	case "debug":
		logger.Debugf("%s", string(rawPayload))
	default:
		logger.Infof("%s", string(rawPayload))
	}
}
