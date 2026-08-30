package kubernetes

import (
	"bytes"
	"context"
	"strings"
)

type logSinkKey struct{}

// LogSink receives one complete line of command output. Used so kind/minikube
// create can show up in the cluster log viewer while the process is still running.
type LogSink func(line string)

// WithLogSink attaches a line sink to ctx. Commander.Run tees stdout/stderr to it.
func WithLogSink(ctx context.Context, sink LogSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, logSinkKey{}, sink)
}

func logSinkFrom(ctx context.Context) LogSink {
	s, _ := ctx.Value(logSinkKey{}).(LogSink)
	return s
}

// WithoutLogSink keeps command output off the cluster log stream. Used when
// the command prints a kubeconfig or other secret.
func WithoutLogSink(ctx context.Context) context.Context {
	return context.WithValue(ctx, logSinkKey{}, LogSink(nil))
}

// maxSinkLineBytes matches the pod log scanner cap: a process that never
// writes a newline must not grow this buffer without bound.
const maxSinkLineBytes = 256 << 10

type lineEmitter struct {
	emit func(string)
	buf  []byte
}

func (l *lineEmitter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			if len(l.buf) > maxSinkLineBytes {
				l.emitLine(string(l.buf[:maxSinkLineBytes]))
				l.buf = l.buf[maxSinkLineBytes:]
			}
			break
		}
		l.emitLine(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

func (l *lineEmitter) Flush() {
	if len(l.buf) == 0 {
		return
	}
	l.emitLine(string(l.buf))
	l.buf = nil
}

func (l *lineEmitter) emitLine(s string) {
	s = strings.TrimRight(s, "\r")
	if s == "" || l.emit == nil {
		return
	}
	l.emit(s)
}
