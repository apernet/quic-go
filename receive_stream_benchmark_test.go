package quic

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/apernet/quic-go/internal/monotime"
	"github.com/apernet/quic-go/internal/protocol"
	"github.com/apernet/quic-go/internal/wire"
)

const (
	receiveStreamBenchmarkFrameCount = 256
	receiveStreamBenchmarkFrameSize  = 1200
	receiveStreamBenchmarkReadSize   = 16 * 1024
	receiveStreamBenchmarkWindow     = 32
)

type receiveStreamBenchmarkScenario struct {
	frames     []*wire.StreamFrame
	order      []int
	readBuffer []byte
}

type receiveStreamBenchmarkResult struct {
	bytesRead       protocol.ByteCount
	flowBytesRead   protocol.ByteCount
	highestReceived protocol.ByteCount
	finalOffset     protocol.ByteCount
	callbacks       int
	completions     int
	err             error
}

type receiveStreamBenchmarkFlowController struct {
	bytesRead       protocol.ByteCount
	highestReceived protocol.ByteCount
	finalOffset     protocol.ByteCount
}

func (f *receiveStreamBenchmarkFlowController) SendWindowSize() protocol.ByteCount {
	return protocol.MaxByteCount
}

func (*receiveStreamBenchmarkFlowController) UpdateSendWindow(protocol.ByteCount) bool { return false }
func (*receiveStreamBenchmarkFlowController) AddBytesSent(protocol.ByteCount)          {}
func (*receiveStreamBenchmarkFlowController) GetWindowUpdate(monotime.Time) protocol.ByteCount {
	return 0
}

func (f *receiveStreamBenchmarkFlowController) AddBytesRead(n protocol.ByteCount) (bool, bool) {
	f.bytesRead += n
	return false, false
}

func (f *receiveStreamBenchmarkFlowController) UpdateHighestReceived(
	offset protocol.ByteCount,
	final bool,
	_ monotime.Time,
) error {
	if offset > f.highestReceived {
		f.highestReceived = offset
	}
	if final {
		f.finalOffset = offset
	}
	return nil
}

func (*receiveStreamBenchmarkFlowController) Abandon()             {}
func (*receiveStreamBenchmarkFlowController) IsNewlyBlocked() bool { return false }

type receiveStreamBenchmarkSender struct {
	completions int
}

func (*receiveStreamBenchmarkSender) onHasConnectionData() {}
func (*receiveStreamBenchmarkSender) onHasStreamData(protocol.StreamID, *SendStream) {
}
func (*receiveStreamBenchmarkSender) onHasStreamControlFrame(protocol.StreamID, streamControlFrameGetter) {
}
func (s *receiveStreamBenchmarkSender) onStreamCompleted(protocol.StreamID) {
	s.completions++
}

func BenchmarkReceiveStreamHandleAndReadSequential(b *testing.B) {
	benchmarkReceiveStreamHandleAndRead(b, newReceiveStreamBenchmarkScenario(false))
}

func BenchmarkReceiveStreamHandleAndReadReorderedWindow32(b *testing.B) {
	benchmarkReceiveStreamHandleAndRead(b, newReceiveStreamBenchmarkScenario(true))
}

func newReceiveStreamBenchmarkScenario(reordered bool) *receiveStreamBenchmarkScenario {
	frames := make([]*wire.StreamFrame, receiveStreamBenchmarkFrameCount)
	for frameIndex := range frames {
		offset := frameIndex * receiveStreamBenchmarkFrameSize
		data := make([]byte, receiveStreamBenchmarkFrameSize)
		for dataIndex := range data {
			data[dataIndex] = byte((offset + dataIndex) % 251)
		}
		frames[frameIndex] = &wire.StreamFrame{
			Offset: protocol.ByteCount(offset),
			Data:   data,
			Fin:    frameIndex == receiveStreamBenchmarkFrameCount-1,
		}
	}

	order := make([]int, 0, receiveStreamBenchmarkFrameCount)
	for windowStart := 0; windowStart < receiveStreamBenchmarkFrameCount; windowStart += receiveStreamBenchmarkWindow {
		if reordered {
			for frameIndex := windowStart + receiveStreamBenchmarkWindow/2; frameIndex < windowStart+receiveStreamBenchmarkWindow; frameIndex++ {
				order = append(order, frameIndex)
			}
		}
		for frameIndex := windowStart; frameIndex < windowStart+receiveStreamBenchmarkWindow/2; frameIndex++ {
			order = append(order, frameIndex)
		}
		if !reordered {
			for frameIndex := windowStart + receiveStreamBenchmarkWindow/2; frameIndex < windowStart+receiveStreamBenchmarkWindow; frameIndex++ {
				order = append(order, frameIndex)
			}
		}
	}

	return &receiveStreamBenchmarkScenario{
		frames:     frames,
		order:      order,
		readBuffer: make([]byte, receiveStreamBenchmarkReadSize),
	}
}

func benchmarkReceiveStreamHandleAndRead(b *testing.B, scenario *receiveStreamBenchmarkScenario) {
	b.Helper()
	wantBytes := protocol.ByteCount(receiveStreamBenchmarkFrameCount * receiveStreamBenchmarkFrameSize)

	validation := scenario.run(true)
	if validation.err != nil {
		b.Fatalf("validating receive stream benchmark: %v", validation.err)
	}
	if validation.bytesRead != wantBytes || validation.flowBytesRead != wantBytes {
		b.Fatalf("validation read %d bytes, flow controller recorded %d; want %d", validation.bytesRead, validation.flowBytesRead, wantBytes)
	}
	if validation.highestReceived != wantBytes || validation.finalOffset != wantBytes {
		b.Fatalf("validation highest offset %d, final offset %d; want %d", validation.highestReceived, validation.finalOffset, wantBytes)
	}
	if validation.callbacks != receiveStreamBenchmarkFrameCount {
		b.Fatalf("validation released %d frame callbacks; want %d", validation.callbacks, receiveStreamBenchmarkFrameCount)
	}
	if validation.completions != 1 {
		b.Fatalf("validation completed stream %d times; want 1", validation.completions)
	}

	b.ReportAllocs()
	b.SetBytes(int64(wantBytes))
	for b.Loop() {
		result := scenario.run(false)
		if result.err != nil {
			b.Fatalf("running receive stream benchmark: %v", result.err)
		}
		if result.bytesRead != wantBytes || result.flowBytesRead != wantBytes || result.completions != 1 {
			b.Fatalf("benchmark lifecycle mismatch: read=%d flow=%d completions=%d", result.bytesRead, result.flowBytesRead, result.completions)
		}
	}
}

func (s *receiveStreamBenchmarkScenario) run(validate bool) receiveStreamBenchmarkResult {
	flowController := &receiveStreamBenchmarkFlowController{}
	sender := &receiveStreamBenchmarkSender{}
	stream := newReceiveStream(42, sender, flowController)
	now := monotime.Now()
	var bytesRead protocol.ByteCount
	var callbacks int

	for windowStart := 0; windowStart < len(s.order); windowStart += receiveStreamBenchmarkWindow {
		for _, frameIndex := range s.order[windowStart : windowStart+receiveStreamBenchmarkWindow] {
			frame := s.frames[frameIndex]
			if err := stream.handleStreamFrame(frame, now); err != nil {
				return receiveStreamBenchmarkResult{err: fmt.Errorf("handling frame %d: %w", frameIndex, err)}
			}
			if validate {
				entry, ok := stream.frameQueue.queue[frame.Offset]
				if !ok {
					return receiveStreamBenchmarkResult{err: fmt.Errorf("frame %d missing from sorter queue", frameIndex)}
				}
				originalCallback := entry.DoneCb
				entry.DoneCb = func() {
					callbacks++
					if originalCallback != nil {
						originalCallback()
					}
				}
				stream.frameQueue.queue[frame.Offset] = entry
			}
		}

		windowBytesRemaining := receiveStreamBenchmarkWindow * receiveStreamBenchmarkFrameSize
		for windowBytesRemaining > 0 {
			readSize := min(windowBytesRemaining, len(s.readBuffer))
			n, err := stream.Read(s.readBuffer[:readSize])
			isFinalRead := bytesRead+protocol.ByteCount(n) == protocol.ByteCount(receiveStreamBenchmarkFrameCount*receiveStreamBenchmarkFrameSize)
			if n != readSize {
				return receiveStreamBenchmarkResult{err: fmt.Errorf("read %d bytes at offset %d; want %d", n, bytesRead, readSize)}
			}
			if isFinalRead {
				if !errors.Is(err, io.EOF) {
					return receiveStreamBenchmarkResult{err: fmt.Errorf("final read error: %w", err)}
				}
			} else if err != nil {
				return receiveStreamBenchmarkResult{err: fmt.Errorf("reading at offset %d: %w", bytesRead, err)}
			}
			if validate {
				for dataIndex, value := range s.readBuffer[:n] {
					want := byte((int(bytesRead) + dataIndex) % 251)
					if value != want {
						return receiveStreamBenchmarkResult{err: fmt.Errorf("byte at offset %d is %d; want %d", int(bytesRead)+dataIndex, value, want)}
					}
				}
			}
			bytesRead += protocol.ByteCount(n)
			windowBytesRemaining -= n
		}
	}

	return receiveStreamBenchmarkResult{
		bytesRead:       bytesRead,
		flowBytesRead:   flowController.bytesRead,
		highestReceived: flowController.highestReceived,
		finalOffset:     flowController.finalOffset,
		callbacks:       callbacks,
		completions:     sender.completions,
	}
}
