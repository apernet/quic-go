package quic

import (
	"testing"

	"github.com/apernet/quic-go/internal/protocol"
)

const (
	benchmarkFrameCount = 256
	benchmarkFrameSize  = 1200
)

func BenchmarkFrameSorterPushOrdered(b *testing.B) {
	payload := make([]byte, benchmarkFrameSize)
	b.ReportAllocs()
	b.SetBytes(int64(benchmarkFrameCount * benchmarkFrameSize))

	for b.Loop() {
		sorter := newFrameSorter()
		for index := range benchmarkFrameCount {
			offset := protocol.ByteCount(index * benchmarkFrameSize)
			if err := sorter.Push(payload, offset, nil); err != nil {
				b.Fatal(err)
			}
			poppedOffset, data, _ := sorter.Pop()
			if poppedOffset != offset || len(data) != benchmarkFrameSize {
				b.Fatalf("popped (%d, %d bytes), expected (%d, %d bytes)", poppedOffset, len(data), offset, benchmarkFrameSize)
			}
		}
	}
}

func BenchmarkFrameSorterPushReordered(b *testing.B) {
	payload := make([]byte, benchmarkFrameSize)
	b.ReportAllocs()
	b.SetBytes(int64(benchmarkFrameCount * benchmarkFrameSize))

	for b.Loop() {
		sorter := newFrameSorter()
		var callbacks int
		callback := func() { callbacks++ }
		for windowStart := 0; windowStart < benchmarkFrameCount; windowStart += 32 {
			for index := windowStart + 16; index < windowStart+32; index++ {
				offset := protocol.ByteCount(index * benchmarkFrameSize)
				if err := sorter.Push(payload, offset, callback); err != nil {
					b.Fatal(err)
				}
			}
			for index := windowStart; index < windowStart+16; index++ {
				offset := protocol.ByteCount(index * benchmarkFrameSize)
				if err := sorter.Push(payload, offset, callback); err != nil {
					b.Fatal(err)
				}
			}
			for index := windowStart; index < windowStart+32; index++ {
				expectedOffset := protocol.ByteCount(index * benchmarkFrameSize)
				poppedOffset, data, done := sorter.Pop()
				if poppedOffset != expectedOffset || len(data) != benchmarkFrameSize || done == nil {
					b.Fatalf("popped (%d, %d bytes), expected (%d, %d bytes)", poppedOffset, len(data), expectedOffset, benchmarkFrameSize)
				}
				done()
			}
		}
		if callbacks != benchmarkFrameCount {
			b.Fatalf("callbacks = %d, expected %d", callbacks, benchmarkFrameCount)
		}
	}
}
