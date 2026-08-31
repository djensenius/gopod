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
