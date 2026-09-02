package podcast

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatChapterRangeDoesNotAccumulatePriorRanges(t *testing.T) {
	tests := []struct {
		chapterStart int
		chapterEnd   int
		wantStart    string
		wantEnd      string
	}{
		{chapterStart: 0, chapterEnd: 10, wantStart: "00:00:00", wantEnd: "00:00:10"},
		{chapterStart: 10, chapterEnd: 20, wantStart: "00:00:10", wantEnd: "00:00:20"},
		{chapterStart: 20, chapterEnd: 30, wantStart: "00:00:20", wantEnd: "00:00:30"},
	}

	for _, test := range tests {
		gotStart, gotEnd := formatChapterRange(test.chapterStart, test.chapterEnd)
		if gotStart != test.wantStart || gotEnd != test.wantEnd {
			t.Fatalf(
				"formatChapterRange(%d, %d) = (%q, %q), want (%q, %q)",
				test.chapterStart,
				test.chapterEnd,
				gotStart,
				gotEnd,
				test.wantStart,
				test.wantEnd,
			)
		}
	}
}

func TestRecordingElapsedSecondUsesWallTimeAndCapsAtDuration(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	duration := 4 * time.Second
	tests := []struct {
		name string
		now  time.Time
		want int
	}{
		{name: "before start", now: start.Add(-time.Second), want: 0},
		{name: "subsecond", now: start.Add(999 * time.Millisecond), want: 0},
		{name: "elapsed seconds", now: start.Add(2500 * time.Millisecond), want: 2},
		{name: "duration cap", now: start.Add(10 * time.Second), want: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := recordingElapsedSecond(start, test.now, duration)
			if got != test.want {
				t.Fatalf("got elapsed second %d, want %d", got, test.want)
			}
		})
	}
}

func TestMonitorStreamUsesElapsedTimeForSlowMetadataReads(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		requestCount++
		streamTitle := "Second"
		if requestCount == 1 {
			time.Sleep(2100 * time.Millisecond)
			streamTitle = "First"
		}

		metadata := []byte("StreamTitle='" + streamTitle + "';")
		padding := 16 - len(metadata)%16
		if padding != 16 {
			metadata = append(metadata, make([]byte, padding)...)
		}
		w.Header().Set("icy-metaint", "1")
		_, _ = w.Write([]byte{'x', byte(len(metadata) / 16)})
		_, _ = w.Write(metadata)
	}))
	t.Cleanup(server.Close)

	metadataPath, descriptionPath, err := MonitorStream(
		context.Background(),
		server.URL,
		4*time.Second,
		"Timing Test",
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(metadataPath)
		_ = os.Remove(descriptionPath)
	})

	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"START=1\nEND=2\ntitle=First",
		"START=3\nEND=4\ntitle=Second",
	} {
		if !strings.Contains(string(metadata), want) {
			t.Fatalf("metadata %q does not contain %q", metadata, want)
		}
	}

	description, err := os.ReadFile(descriptionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[00:00:00 - 00:00:02]: First",
		"[00:00:02 - 00:00:04]: Second",
	} {
		if !strings.Contains(string(description), want) {
			t.Fatalf("description %q does not contain %q", description, want)
		}
	}
}

func TestGetStreamTitleHandlesShortMetadataSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metadata := []byte("x;StreamTitle='Example Show';")
		padding := 16 - len(metadata)%16
		if padding != 16 {
			metadata = append(metadata, make([]byte, padding)...)
		}

		w.Header().Set("icy-metaint", "1")
		_, _ = w.Write([]byte{'x', byte(len(metadata) / 16)})
		_, _ = w.Write(metadata)
	}))
	t.Cleanup(server.Close)

	title, err := GetStreamTitle(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Example Show" {
		t.Fatalf("got title %q, want %q", title, "Example Show")
	}
}

func TestGetStreamTitleHandlesMetadataLargerThan255Bytes(
	t *testing.T,
) {
	title := strings.Repeat("Long metadata title ", 12)
	metadata := []byte("StreamTitle='" + title + "';")
	padding := 16 - len(metadata)%16
	if padding != 16 {
		metadata = append(metadata, make([]byte, padding)...)
	}
	if len(metadata) <= 255 {
		t.Fatalf("test metadata length = %d, want more than 255", len(metadata))
	}

	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		w.Header().Set("icy-metaint", "1")
		_, _ = w.Write([]byte{'x', byte(len(metadata) / 16)})
		_, _ = w.Write(metadata)
	}))
	t.Cleanup(server.Close)

	got, err := GetStreamTitle(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got != title {
		t.Fatalf("got title %q, want %q", got, title)
	}
}

func TestGetStreamTitleReportsRequestFailure(t *testing.T) {
	_, err := GetStreamTitle(context.Background(), "://invalid")
	if err == nil {
		t.Fatal("expected request error")
	}
	if !strings.Contains(err.Error(), "missing protocol scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetStreamTitleHonorsCancellation(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(
		_ http.ResponseWriter,
		request *http.Request,
	) {
		requestStarted <- struct{}{}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := GetStreamTitle(ctx, server.URL)
		result <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata request did not start")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got error %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("metadata request did not stop after cancellation")
	}
}
