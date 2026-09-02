package podcast

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
