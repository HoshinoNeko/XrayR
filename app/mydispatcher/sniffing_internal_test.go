package mydispatcher

import (
	"github.com/xtls/xray-core/common/buf"
	"io"
	"testing"
	"time"
)

type delayedReader struct{ ready chan struct{} }

func (r *delayedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	<-r.ready
	return buf.MergeBytes(nil, []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")), io.EOF
}

func TestSniffingTimeoutPreservesGenericReaderPayload(t *testing.T) {
	source := &delayedReader{ready: make(chan struct{})}
	reader := &cachedReader{reader: source}
	defer reader.Interrupt()
	if _, err := reader.readTimeout(time.Millisecond); err != buf.ErrReadTimeout {
		t.Fatal(err)
	}
	close(source.ready)
	payload := buf.New()
	defer payload.Release()
	reader.Cache(payload)
	if payload.Len() == 0 {
		t.Fatal("sniffing lost payload")
	}
	mb, err := reader.ReadMultiBuffer()
	defer buf.ReleaseMulti(mb)
	if err != nil || mb.Len() != payload.Len() {
		t.Fatalf("payload replay: %v %d", err, mb.Len())
	}
	if _, err = reader.ReadMultiBuffer(); err != io.EOF {
		t.Fatalf("EOF lost: %v", err)
	}
}
