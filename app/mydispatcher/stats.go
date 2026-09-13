package mydispatcher

import (
	"io"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/features/stats"
)

type AuthorizedReader struct {
	Allowed func() bool
	Reader  buf.Reader
}

func (r *AuthorizedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	if !r.Allowed() {
		common.Interrupt(r.Reader)
		return nil, io.ErrClosedPipe
	}
	return r.Reader.ReadMultiBuffer()
}

func (r *AuthorizedReader) Close() error { return common.Close(r.Reader) }
func (r *AuthorizedReader) Interrupt()   { common.Interrupt(r.Reader) }

type AuthorizedWriter struct {
	Allowed func() bool
	Writer  buf.Writer
}

func (w *AuthorizedWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if !w.Allowed() {
		buf.ReleaseMulti(mb)
		common.Interrupt(w.Writer)
		return io.ErrClosedPipe
	}
	return w.Writer.WriteMultiBuffer(mb)
}

func (w *AuthorizedWriter) Close() error { return common.Close(w.Writer) }
func (w *AuthorizedWriter) Interrupt()   { common.Interrupt(w.Writer) }

type SizeStatWriter struct {
	Counter stats.Counter
	Writer  buf.Writer
}

type SizeStatReader struct {
	Counter stats.Counter
	Reader  buf.Reader
}

func (r *SizeStatReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if !mb.IsEmpty() {
		r.Counter.Add(int64(mb.Len()))
	}
	return mb, err
}

func (r *SizeStatReader) Close() error {
	return common.Close(r.Reader)
}

func (r *SizeStatReader) Interrupt() {
	common.Interrupt(r.Reader)
}

func (w *SizeStatWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.Counter.Add(int64(mb.Len()))
	return w.Writer.WriteMultiBuffer(mb)
}

func (w *SizeStatWriter) Close() error {
	return common.Close(w.Writer)
}

func (w *SizeStatWriter) Interrupt() {
	common.Interrupt(w.Writer)
}
