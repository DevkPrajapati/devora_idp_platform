package kubernetes

import (
	"strings"
	"testing"
)

func TestParseLogLineSplitsKubeletTimestamp(t *testing.T) {
	line := ParseLogLine("api-7d9f", "2026-07-28T10:15:00.123456789Z listening on :8080")

	if line.PodName != "api-7d9f" {
		t.Errorf("PodName = %q", line.PodName)
	}
	if line.Timestamp != "2026-07-28T10:15:00.123456789Z" {
		t.Errorf("Timestamp = %q", line.Timestamp)
	}
	if line.Message != "listening on :8080" {
		t.Errorf("Message = %q", line.Message)
	}
}

func TestParseLogLinePreservesMessageContent(t *testing.T) {
	// Only the first space separates the timestamp; everything after it is the
	// message, spaces and all.
	line := ParseLogLine("p", "2026-07-28T10:15:00Z GET /api/v1/health 200 1.45ms")
	if line.Message != "GET /api/v1/health 200 1.45ms" {
		t.Errorf("Message = %q, want the full remainder", line.Message)
	}

	// A message that itself looks like a timestamp must not be re-split.
	line = ParseLogLine("p", "2026-07-28T10:15:00Z 2026-07-28T10:15:00Z duplicated")
	if line.Message != "2026-07-28T10:15:00Z duplicated" {
		t.Errorf("Message = %q", line.Message)
	}
}

// A line without a kubelet timestamp must be passed through whole. Eating its
// first word as a timestamp would silently corrupt the output.
func TestParseLogLineWithoutTimestamp(t *testing.T) {
	cases := []string{
		"plain message with no timestamp",
		"ERROR something failed",
		"",
		"single-token",
		// Close to a timestamp but not one — must not be consumed.
		"2026-07-28 10:15:00 not rfc3339",
		"12345 numeric prefix",
	}

	for _, raw := range cases {
		line := ParseLogLine("p", raw)
		if line.Timestamp != "" {
			t.Errorf("ParseLogLine(%q) extracted timestamp %q", raw, line.Timestamp)
		}
		if line.Message != raw {
			t.Errorf("ParseLogLine(%q) message = %q, want the line unchanged", raw, line.Message)
		}
	}
}

func TestParseLogLineTrimsCarriageReturn(t *testing.T) {
	// Containers writing CRLF would otherwise leave a stray \r that renders as
	// a blank glyph in the browser.
	line := ParseLogLine("p", "2026-07-28T10:15:00Z windows line\r")
	if strings.HasSuffix(line.Message, "\r") {
		t.Errorf("carriage return survived: %q", line.Message)
	}
	if line.Message != "windows line" {
		t.Errorf("Message = %q", line.Message)
	}
}

func TestLooksLikeTimestamp(t *testing.T) {
	valid := []string{
		"2026-07-28T10:15:00Z",
		"2026-07-28T10:15:00.123456789Z",
		"2026-07-28T10:15:00+02:00",
		"2026-07-28T10:15:00.123-05:00",
	}
	for _, token := range valid {
		if !looksLikeTimestamp(token) {
			t.Errorf("looksLikeTimestamp(%q) = false, want true", token)
		}
	}

	invalid := []string{
		"",
		"short",
		"2026-07-28",           // date only, too short
		"2026/07/28T10:15:00Z", // wrong separators
		"2026-07-28 10:15:00Z", // space instead of T
		strings.Repeat("2026-07-28T10:15:00Z", 3), // too long
	}
	for _, token := range invalid {
		if looksLikeTimestamp(token) {
			t.Errorf("looksLikeTimestamp(%q) = true, want false", token)
		}
	}
}

func TestLogStreamTailBounds(t *testing.T) {
	// The clamping in StreamPodLogs guards two real failures: a zero tail would
	// ask the kubelet for the entire history of a long-running pod, and an
	// unbounded one lets a client request it deliberately.
	if defaultTailLines <= 0 {
		t.Error("defaultTailLines must be positive")
	}
	if maxTailLines < defaultTailLines {
		t.Error("maxTailLines must not be below the default")
	}
	if maxLogLineBytes <= 0 {
		t.Error("maxLogLineBytes must be positive")
	}
}
