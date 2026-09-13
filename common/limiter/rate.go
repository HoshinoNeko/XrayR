package limiter

import (
	"context"
	"fmt"
	"io"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

type Writer struct {
	writer  buf.Writer
	limiter *rate.Limiter
	w       io.Writer
}

type Reader struct {
	reader  buf.Reader
	limiter *rate.Limiter
}

func (l *Limiter) RateReader(reader buf.Reader, limiter *rate.Limiter) buf.Reader {
	return &Reader{reader: reader, limiter: limiter}
}

func (r *Reader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.reader.ReadMultiBuffer()
	if !mb.IsEmpty() {
		if waitErr := waitBytes(r.limiter, int(mb.Len())); waitErr != nil {
			buf.ReleaseMulti(mb)
			return nil, waitErr
		}
	}
	return mb, err
}

func (r *Reader) Close() error {
	return common.Close(r.reader)
}

func (r *Reader) Interrupt() {
	common.Interrupt(r.reader)
}

func (l *Limiter) RateWriter(writer buf.Writer, limiter *rate.Limiter) buf.Writer {
	return &Writer{
		writer:  writer,
		limiter: limiter,
	}
}

func (w *Writer) Close() error {
	return common.Close(w.writer)
}

func (w *Writer) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if err := waitBytes(w.limiter, int(mb.Len())); err != nil {
		buf.ReleaseMulti(mb)
		return err
	}
	return w.writer.WriteMultiBuffer(mb)
}

func waitBytes(limiter *rate.Limiter, size int) error {
	for size > 0 {
		chunk := size
		if burst := limiter.Burst(); chunk > burst {
			chunk = burst
		}
		if chunk <= 0 {
			return fmt.Errorf("rate limiter has no burst capacity")
		}
		if err := limiter.WaitN(context.Background(), chunk); err != nil {
			return err
		}
		size -= chunk
	}
	return nil
}
