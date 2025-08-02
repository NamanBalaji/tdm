package peer_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/NamanBalaji/tdm/pkg/torrent/peer"
	"io"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, w *peer.Writer, writeFunc func() error) {
	t.Helper()
	if err := writeFunc(); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}
}

func mustRead(t *testing.T, r *peer.Reader) peer.Message {
	t.Helper()
	msg, err := r.ReadMsg()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	return msg
}

func newReaderWriter(t *testing.T) (*peer.Reader, *peer.Writer, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return peer.NewReader(buf), peer.NewWriter(buf), buf
}

func TestMessageRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		writeFunc func(*peer.Writer) error
		verify    func(*testing.T, peer.Message)
	}{
		{
			name:      "keep-alive",
			writeFunc: (*peer.Writer).WriteKeepAlive,
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 10 {
					t.Errorf("expected keep-alive type 10, got %d", msg.Type)
				}
				if len(msg.Payload) != 0 {
					t.Errorf("keep-alive should have empty payload")
				}
			},
		},
		{
			name:      "choke",
			writeFunc: (*peer.Writer).WriteChoke,
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 0 {
					t.Errorf("expected choke type 0, got %d", msg.Type)
				}
				if len(msg.Payload) != 0 {
					t.Errorf("choke should have empty payload")
				}
			},
		},
		{
			name:      "unchoke",
			writeFunc: (*peer.Writer).WriteUnchoke,
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 1 {
					t.Errorf("expected unchoke type 1, got %d", msg.Type)
				}
			},
		},
		{
			name:      "interested",
			writeFunc: (*peer.Writer).WriteInterested,
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 2 {
					t.Errorf("expected interested type 2, got %d", msg.Type)
				}
			},
		},
		{
			name:      "not-interested",
			writeFunc: (*peer.Writer).WriteNotInterested,
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 3 {
					t.Errorf("expected not-interested type 3, got %d", msg.Type)
				}
			},
		},
		{
			name:      "have",
			writeFunc: func(w *peer.Writer) error { return w.WriteHave(12345) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 4 {
					t.Errorf("expected have type 4, got %d", msg.Type)
				}
				if msg.Index != 12345 {
					t.Errorf("expected index 12345, got %d", msg.Index)
				}
			},
		},
		{
			name:      "bitfield-empty",
			writeFunc: func(w *peer.Writer) error { return w.WriteBitfield(nil) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 5 {
					t.Errorf("expected bitfield type 5, got %d", msg.Type)
				}
				if len(msg.Payload) != 0 {
					t.Errorf("expected empty payload, got %d bytes", len(msg.Payload))
				}
			},
		},
		{
			name:      "bitfield-data",
			writeFunc: func(w *peer.Writer) error { return w.WriteBitfield([]byte{0xAA, 0x55, 0xFF}) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 5 {
					t.Errorf("expected bitfield type 5, got %d", msg.Type)
				}
				expected := []byte{0xAA, 0x55, 0xFF}
				if !bytes.Equal(msg.Payload, expected) {
					t.Errorf("expected payload %v, got %v", expected, msg.Payload)
				}
			},
		},
		{
			name:      "request",
			writeFunc: func(w *peer.Writer) error { return w.WriteRequest(100, 16384, 8192) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 6 {
					t.Errorf("expected request type 6, got %d", msg.Type)
				}
				if msg.Index != 100 || msg.Begin != 16384 || msg.Length != 8192 {
					t.Errorf("expected (100,16384,8192), got (%d,%d,%d)",
						msg.Index, msg.Begin, msg.Length)
				}
			},
		},
		{
			name:      "piece",
			writeFunc: func(w *peer.Writer) error { return w.WritePiece(50, 8192, []byte("test data")) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 7 {
					t.Errorf("expected piece type 7, got %d", msg.Type)
				}
				if msg.Index != 50 || msg.Begin != 8192 {
					t.Errorf("expected (50,8192), got (%d,%d)", msg.Index, msg.Begin)
				}
				if !bytes.Equal(msg.Block, []byte("test data")) {
					t.Errorf("expected block 'test data', got %v", msg.Block)
				}
			},
		},
		{
			name:      "cancel",
			writeFunc: func(w *peer.Writer) error { return w.WriteCancel(200, 32768, 4096) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 8 {
					t.Errorf("expected cancel type 8, got %d", msg.Type)
				}
				if msg.Index != 200 || msg.Begin != 32768 || msg.Length != 4096 {
					t.Errorf("expected (200,32768,4096), got (%d,%d,%d)",
						msg.Index, msg.Begin, msg.Length)
				}
			},
		},
		{
			name:      "port",
			writeFunc: func(w *peer.Writer) error { return w.WritePort(6881) },
			verify: func(t *testing.T, msg peer.Message) {
				if msg.Type != 9 {
					t.Errorf("expected port type 9, got %d", msg.Type)
				}
				if msg.Port != 6881 {
					t.Errorf("expected port 6881, got %d", msg.Port)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer, _ := newReaderWriter(t)
			mustWrite(t, writer, func() error { return tt.writeFunc(writer) })
			msg := mustRead(t, reader)
			tt.verify(t, msg)
		})
	}
}

func TestWireFormat(t *testing.T) {
	tests := []struct {
		name       string
		writeFunc  func(*peer.Writer) error
		expectWire []byte
	}{
		{
			name:       "keep-alive wire format",
			writeFunc:  (*peer.Writer).WriteKeepAlive,
			expectWire: []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name:       "choke wire format",
			writeFunc:  (*peer.Writer).WriteChoke,
			expectWire: []byte{0x00, 0x00, 0x00, 0x01, 0x00},
		},
		{
			name:       "have wire format",
			writeFunc:  func(w *peer.Writer) error { return w.WriteHave(42) },
			expectWire: []byte{0x00, 0x00, 0x00, 0x05, 0x04, 0x00, 0x00, 0x00, 0x2A},
		},
		{
			name:       "port wire format",
			writeFunc:  func(w *peer.Writer) error { return w.WritePort(6881) },
			expectWire: []byte{0x00, 0x00, 0x00, 0x03, 0x09, 0x1A, 0xE1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := peer.NewWriter(&buf)
			mustWrite(t, writer, func() error { return tt.writeFunc(writer) })

			if !bytes.Equal(buf.Bytes(), tt.expectWire) {
				t.Errorf("wire format mismatch:\ngot:  %x\nwant: %x",
					buf.Bytes(), tt.expectWire)
			}
		})
	}
}

func TestErrorConditions(t *testing.T) {
	t.Run("message too big", func(t *testing.T) {
		var buf bytes.Buffer
		reader := peer.NewReader(&buf)

		maxLen := uint32(1 << 20)
		binary.Write(&buf, binary.BigEndian, maxLen+1)

		_, err := reader.ReadMsg()
		if err == nil || !strings.Contains(err.Error(), "larger than 1 MiB") {
			t.Errorf("expected 'larger than 1 MiB' error, got: %v", err)
		}
	})

	t.Run("payload too short", func(t *testing.T) {
		payloadTests := []struct {
			msgType     byte
			payload     []byte
			expectError bool
		}{
			{4, []byte{1, 2, 3}, true},
			{4, []byte{1, 2, 3, 4}, false},
			{6, []byte{1, 2, 3}, true},
			{6, make([]byte, 12), false},
			{7, []byte{1, 2, 3}, true},
			{7, make([]byte, 8), false},
			{8, []byte{1, 2}, true},
			{8, make([]byte, 12), false},
			{9, []byte{1}, true},
			{9, []byte{0x1A, 0xE1}, false},
			{5, []byte{}, false},
			{5, []byte{0xFF, 0x00}, false},
		}

		for _, tt := range payloadTests {
			t.Run("", func(t *testing.T) {
				var buf bytes.Buffer

				l := uint32(1 + len(tt.payload))
				binary.Write(&buf, binary.BigEndian, l)
				buf.WriteByte(tt.msgType)
				buf.Write(tt.payload)

				reader := peer.NewReader(&buf)
				_, err := reader.ReadMsg()

				if tt.expectError {
					if err == nil || !strings.Contains(err.Error(), "too short") {
						t.Errorf("expected 'too short' error, got: %v", err)
					}
				} else if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("io errors", func(t *testing.T) {
		reader := peer.NewReader(strings.NewReader("ab"))
		_, err := reader.ReadMsg()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("expected ErrUnexpectedEOF, got: %v", err)
		}

		var buf bytes.Buffer
		binary.Write(&buf, binary.BigEndian, uint32(5))
		buf.Write([]byte{0x04, 0x00})

		reader2 := peer.NewReader(&buf)
		_, err = reader2.ReadMsg()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("expected ErrUnexpectedEOF, got: %v", err)
		}
	})
}

func TestBufferSafety(t *testing.T) {
	t.Run("payload invalidation", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		mustWrite(t, writer, func() error { return writer.WriteBitfield([]byte("first")) })
		mustWrite(t, writer, func() error { return writer.WriteBitfield([]byte("second")) })

		msg1 := mustRead(t, reader)
		payload1 := msg1.Payload
		payload1Copy := make([]byte, len(payload1))
		copy(payload1Copy, payload1)

		msg2 := mustRead(t, reader)

		if !bytes.Equal(payload1Copy, []byte("first")) {
			t.Errorf("payload copy corrupted: got %q, want %q", payload1Copy, "first")
		}

		if !bytes.Equal(msg2.Payload, []byte("second")) {
			t.Errorf("second payload wrong: got %q, want %q", msg2.Payload, "second")
		}
	})

	t.Run("block invalidation", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		mustWrite(t, writer, func() error { return writer.WritePiece(1, 0, []byte("block1")) })
		mustWrite(t, writer, func() error { return writer.WritePiece(2, 0, []byte("block2")) })

		msg1 := mustRead(t, reader)
		block1 := msg1.Block
		block1Copy := make([]byte, len(block1))
		copy(block1Copy, block1)

		msg2 := mustRead(t, reader)

		if !bytes.Equal(block1Copy, []byte("block1")) {
			t.Errorf("block copy corrupted: got %q, want %q", block1Copy, "block1")
		}
		if !bytes.Equal(msg2.Block, []byte("block2")) {
			t.Errorf("second block wrong: got %q, want %q", msg2.Block, "block2")
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("maximum values", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		mustWrite(t, writer, func() error {
			return writer.WriteRequest(0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF)
		})

		msg := mustRead(t, reader)
		if msg.Index != 0xFFFFFFFF || msg.Begin != 0xFFFFFFFF || msg.Length != 0xFFFFFFFF {
			t.Errorf("max values failed: got (%x,%x,%x)", msg.Index, msg.Begin, msg.Length)
		}
	})

	t.Run("zero values", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		mustWrite(t, writer, func() error { return writer.WriteHave(0) })
		mustWrite(t, writer, func() error { return writer.WritePort(0) })
		mustWrite(t, writer, func() error { return writer.WriteRequest(0, 0, 0) })

		msg1 := mustRead(t, reader)
		if msg1.Index != 0 {
			t.Errorf("have index should be 0, got %d", msg1.Index)
		}

		msg2 := mustRead(t, reader)
		if msg2.Port != 0 {
			t.Errorf("port should be 0, got %d", msg2.Port)
		}

		msg3 := mustRead(t, reader)
		if msg3.Index != 0 || msg3.Begin != 0 || msg3.Length != 0 {
			t.Errorf("request should be all zeros, got (%d,%d,%d)",
				msg3.Index, msg3.Begin, msg3.Length)
		}
	})

	t.Run("large piece", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		largeBlock := make([]byte, 65536)
		for i := range largeBlock {
			largeBlock[i] = byte(i % 256)
		}

		mustWrite(t, writer, func() error {
			return writer.WritePiece(999, 131072, largeBlock)
		})

		msg := mustRead(t, reader)
		if msg.Index != 999 || msg.Begin != 131072 {
			t.Errorf("large piece metadata wrong: got (%d,%d)", msg.Index, msg.Begin)
		}
		if !bytes.Equal(msg.Block, largeBlock) {
			t.Errorf("large block data corrupted")
		}
	})

	t.Run("maximum message size", func(t *testing.T) {
		reader, writer, _ := newReaderWriter(t)

		maxBlock := make([]byte, (1<<20)-8-1)
		for i := range maxBlock {
			maxBlock[i] = byte(i % 256)
		}

		mustWrite(t, writer, func() error {
			return writer.WritePiece(42, 0, maxBlock)
		})

		msg := mustRead(t, reader)
		if msg.Index != 42 {
			t.Errorf("max message index wrong: got %d", msg.Index)
		}
		if len(msg.Block) != len(maxBlock) {
			t.Errorf("max message block size wrong: got %d, want %d", len(msg.Block), len(maxBlock))
		}
	})
}

func TestUnknownMessageType(t *testing.T) {
	var buf bytes.Buffer
	reader := peer.NewReader(&buf)

	unknownType := byte(99)
	payload := []byte("unknown payload")
	l := uint32(1 + len(payload))
	binary.Write(&buf, binary.BigEndian, l)
	buf.WriteByte(unknownType)
	buf.Write(payload)

	msg := mustRead(t, reader)
	if msg.Type != unknownType {
		t.Errorf("unknown type should be preserved: got %d, want %d", msg.Type, unknownType)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("unknown payload wrong: got %q", msg.Payload)
	}
}

func TestMessageSequence(t *testing.T) {
	reader, writer, _ := newReaderWriter(t)

	writeSequence := []func() error{
		writer.WriteKeepAlive,
		func() error { return writer.WriteChoke() },
		func() error { return writer.WriteHave(42) },
		func() error { return writer.WriteRequest(10, 20, 30) },
		func() error { return writer.WritePiece(5, 100, []byte("data")) },
		func() error { return writer.WritePort(8080) },
	}

	for i, writeFunc := range writeSequence {
		mustWrite(t, writer, writeFunc)

		msg := mustRead(t, reader)

		expectedTypes := []byte{10, 0, 4, 6, 7, 9} // keep-alive, choke, have, request, piece, port
		if msg.Type != expectedTypes[i] {
			t.Errorf("message %d: wrong type: got %d, want %d", i, msg.Type, expectedTypes[i])
		}
	}
}

func TestWriterIOErrors(t *testing.T) {
	failingWriter := &failingWriter{failAfter: 2}
	writer := peer.NewWriter(failingWriter)

	err := writer.WriteHave(42)
	if err == nil {
		t.Errorf("expected write error, got nil")
	}
}

func TestEmptyPayloadMessages(t *testing.T) {
	reader, writer, _ := newReaderWriter(t)

	emptyMsgs := []func() error{
		writer.WriteChoke,
		writer.WriteUnchoke,
		writer.WriteInterested,
		writer.WriteNotInterested,
	}

	for _, writeFunc := range emptyMsgs {
		mustWrite(t, writer, writeFunc)
		msg := mustRead(t, reader)
		if len(msg.Payload) != 0 {
			t.Errorf("message type %d should have empty payload, got %d bytes",
				msg.Type, len(msg.Payload))
		}
	}
}

type failingWriter struct {
	written   int
	failAfter int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.written >= w.failAfter {
		return 0, io.ErrShortWrite
	}
	n := len(p)
	w.written += n
	if w.written > w.failAfter {
		n = w.failAfter - (w.written - len(p))
		w.written = w.failAfter
		return n, io.ErrShortWrite
	}
	return n, nil
}
