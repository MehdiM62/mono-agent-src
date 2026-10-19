package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Zerolog returns a zerolog.Logger at the root level whose events are
// written through the root slog handler — same format, same destination,
// and the root's run_id — instead of raw JSON on stderr. It replaces
// zerolog.New(os.Stderr).With().Timestamp().Logger().Level(...) in code that
// still takes a zerolog.Logger; add fields with .With() as before. The level
// is read when it is called, so call it after Init.
func Zerolog() zerolog.Logger {
	return zerolog.New(zerologWriter{}).Level(zerologLevel(Level()))
}

// ZerologContext is Zerolog with the run_id WithRunID put in ctx as a
// field (the process's own MONOAGENT_RUN_ID is on the root logger already).
func ZerologContext(ctx context.Context) zerolog.Logger {
	l := Zerolog()
	if id, _ := ctx.Value(runIDKey).(string); id != "" && id != os.Getenv(EnvRunID) {
		l = l.With().Str(KeyRunID, id).Logger()
	}
	return l
}

// demotions are, by Zerolog event message, the Demote calls in effect for
// it, oldest first: the newest one's level applies.
var (
	demoteMu  sync.RWMutex
	demotions = map[string][]*demotion{}
)

type demotion struct{ level slog.Level }

// Demote logs the Zerolog events whose message is one of msgs at level
// instead of their own, filtered by the root level like any slog record. A
// one-shot command that reports those events itself (a run's summary names
// the failed node) uses it to keep them off the terminal at the default
// level. The returned func undoes this call only: a demotion made before or
// after it, and not undone, applies again, whatever the order of the undos.
func Demote(level slog.Level, msgs ...string) (undo func()) {
	d := &demotion{level}
	demoteMu.Lock()
	for _, m := range msgs {
		demotions[m] = append(demotions[m], d)
	}
	demoteMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			demoteMu.Lock()
			defer demoteMu.Unlock()
			for _, m := range msgs {
				list := demotions[m]
				for i, e := range list {
					if e == d {
						list = append(list[:i:i], list[i+1:]...)
						break
					}
				}
				if len(list) == 0 {
					delete(demotions, m)
				} else {
					demotions[m] = list
				}
			}
		})
	}
}

// demotedLevel is the level Demote has msg logged at now, if any.
func demotedLevel(msg string) (slog.Level, bool) {
	demoteMu.RLock()
	defer demoteMu.RUnlock()
	list := demotions[msg]
	if len(list) == 0 {
		return 0, false
	}
	return list[len(list)-1].level, true
}

// zerologLevel maps a slog level onto zerolog's.
func zerologLevel(l slog.Level) zerolog.Level {
	switch {
	case l < slog.LevelDebug:
		return zerolog.TraceLevel
	case l < slog.LevelInfo:
		return zerolog.DebugLevel
	case l < slog.LevelWarn:
		return zerolog.InfoLevel
	case l < slog.LevelError:
		return zerolog.WarnLevel
	}
	return zerolog.ErrorLevel
}

// slogLevel maps a zerolog level onto slog's.
func slogLevel(l zerolog.Level) slog.Level {
	switch l {
	case zerolog.TraceLevel:
		return slog.LevelDebug - 4
	case zerolog.DebugLevel:
		return slog.LevelDebug
	case zerolog.InfoLevel, zerolog.NoLevel:
		return slog.LevelInfo
	case zerolog.WarnLevel:
		return slog.LevelWarn
	}
	return slog.LevelError
}

// zerologWriter turns each zerolog event (one JSON object) into a record of
// the default slog logger.
type zerologWriter struct{}

func (w zerologWriter) Write(p []byte) (int, error) {
	return w.WriteLevel(zerolog.NoLevel, p)
}

func (zerologWriter) WriteLevel(zl zerolog.Level, p []byte) (int, error) {
	// The zerolog logger's own level already chose this event (a caller may
	// lower it for --verbose), so the root level is not applied again.
	level := slogLevel(zl)
	l := slog.Default()
	ctx := context.Background()
	var fields map[string]any
	dec := json.NewDecoder(bytes.NewReader(p))
	dec.UseNumber()
	if err := dec.Decode(&fields); err != nil {
		// Not an event zerolog wrote; pass the text on as it is.
		r := slog.NewRecord(time.Now(), level, string(bytes.TrimSpace(p)), 0)
		return len(p), l.Handler().Handle(ctx, r)
	}
	msg, _ := fields[zerolog.MessageFieldName].(string)
	if v, ok := demotedLevel(msg); ok {
		level = v
		if !l.Enabled(ctx, level) {
			return len(p), nil
		}
	}
	delete(fields, zerolog.MessageFieldName)
	delete(fields, zerolog.LevelFieldName)
	delete(fields, zerolog.TimestampFieldName)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r := slog.NewRecord(time.Now(), level, msg, 0)
	for _, k := range keys {
		r.AddAttrs(slog.Any(k, fields[k]))
	}
	return len(p), l.Handler().Handle(ctx, r)
}
