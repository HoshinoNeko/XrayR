package mydispatcher

import (
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/transport/pipe"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBufferedReaderSniffingDoesNotRequirePipe(t *testing.T) {
	payload := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"
	var source buf.Reader = &buf.BufferedReader{Reader: buf.NewReader(strings.NewReader(payload))}
	// Reproduce the exact historical assertion from the supplied crash log.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("historical pipe assertion did not panic")
			}
		}()
		_ = source.(*pipe.Reader)
	}()
	reader := &cachedReader{reader: source}
	defer reader.Interrupt()
	b := buf.New()
	defer b.Release()
	reader.Cache(b)
	mb, err := reader.ReadMultiBuffer()
	defer buf.ReleaseMulti(mb)
	if err != nil || mb.Len() != int32(len(payload)) {
		t.Fatalf("buffered reader payload lost: bytes=%d err=%v", mb.Len(), err)
	}
}

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
