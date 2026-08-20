package quic

import (
	"github.com/apernet/quic-go/internal/protocol"
	"github.com/apernet/quic-go/internal/utils"
)

func (s *gapSet) matchBlocks(cond utils.ByteInterval, dst []utils.ByteInterval) []utils.ByteInterval {
	blockIndex := s.firstBlockEndingAtOrAfter(cond.Start)
	if blockIndex == len(s.blocks) {
		return dst
	}
	s.hint = blockIndex
	for ; blockIndex < len(s.blocks); blockIndex++ {
		block := s.blocks[blockIndex]
		index := 0
		if blockIndex == s.hint {
			low, high := 0, block.count
			for low < high {
				middle := int(uint(low+high) >> 1)
				if block.gaps[middle].End < cond.Start {
					low = middle + 1
				} else {
					high = middle
				}
			}
			index = low
		}
		for ; index < block.count; index++ {
			gap := block.gaps[index]
			if gap.Start > cond.End {
				return dst
			}
			dst = append(dst, gap)
		}
	}
	return dst
}

func (s *gapSet) firstBlockEndingAtOrAfter(start protocol.ByteCount) int {
	if s.hint >= 0 && s.hint < len(s.blocks) {
		block := s.blocks[s.hint]
		previousEndsBefore := s.hint == 0 || s.blocks[s.hint-1].last().End < start
		if previousEndsBefore && block.last().End >= start {
			return s.hint
		}
	}
	low, high := 0, len(s.blocks)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if s.blocks[middle].last().End < start {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}

func (s *gapSet) insertBlock(value utils.ByteInterval) {
	blockIndex := s.blockForValue(value)
	block := s.blocks[blockIndex]
	index := gapLowerBound(block.values(), value)
	if index < block.count && value.Comp(block.gaps[index]) == 0 {
		block.gaps[index] = value
		s.hint = blockIndex
		return
	}
	if block.count == gapBlockCapacity {
		blockIndex, block, index = s.splitBlock(blockIndex, index)
	}
	copy(block.gaps[index+1:block.count+1], block.gaps[index:block.count])
	block.gaps[index] = value
	block.count++
	s.length++
	s.hint = blockIndex
}

func (s *gapSet) splitBlock(blockIndex, insertionIndex int) (int, *gapBlock, int) {
	block := s.blocks[blockIndex]
	next := new(gapBlock)
	next.count = gapBlockCapacity - gapBlockSplit
	copy(next.gaps[:], block.gaps[gapBlockSplit:gapBlockCapacity])
	for index := gapBlockSplit; index < gapBlockCapacity; index++ {
		block.gaps[index] = utils.ByteInterval{}
	}
	block.count = gapBlockSplit

	s.blocks = append(s.blocks, nil)
	copy(s.blocks[blockIndex+2:], s.blocks[blockIndex+1:])
	s.blocks[blockIndex+1] = next
	if insertionIndex >= gapBlockSplit {
		return blockIndex + 1, next, insertionIndex - gapBlockSplit
	}
	return blockIndex, block, insertionIndex
}

func (s *gapSet) deleteBlock(value utils.ByteInterval) {
	blockIndex, index, found := s.locate(value)
	if !found {
		return
	}
	block := s.blocks[blockIndex]
	copy(block.gaps[index:block.count-1], block.gaps[index+1:block.count])
	block.count--
	block.gaps[block.count] = utils.ByteInterval{}
	s.length--

	if s.length <= gapInlineCapacity {
		s.demote()
		return
	}
	if block.count == 0 {
		s.removeBlock(blockIndex)
		return
	}
	s.mergeSparseBlock(blockIndex)
}

func (s *gapSet) updateBlock(oldValue, newValue utils.ByteInterval) bool {
	blockIndex, index, found := s.locate(oldValue)
	if !found {
		return false
	}
	direction := newValue.Comp(oldValue)
	if direction < 0 {
		if predecessor, ok := s.predecessor(blockIndex, index); ok && newValue.Comp(predecessor) <= 0 {
			return false
		}
	} else if direction > 0 {
		if successor, ok := s.successor(blockIndex, index); ok && newValue.Comp(successor) >= 0 {
			return false
		}
	}
	s.blocks[blockIndex].gaps[index] = newValue
	s.hint = blockIndex
	return true
}

func (s *gapSet) locate(value utils.ByteInterval) (int, int, bool) {
	blockIndex := s.blockForValue(value)
	block := s.blocks[blockIndex]
	index := gapLowerBound(block.values(), value)
	found := index < block.count && value.Comp(block.gaps[index]) == 0
	return blockIndex, index, found
}

func (s *gapSet) blockForValue(value utils.ByteInterval) int {
	if s.hint >= 0 && s.hint < len(s.blocks) {
		previousBefore := s.hint == 0 || s.blocks[s.hint-1].last().Comp(value) < 0
		if previousBefore && s.blocks[s.hint].last().Comp(value) >= 0 {
			return s.hint
		}
	}
	low, high := 0, len(s.blocks)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if s.blocks[middle].last().Comp(value) < 0 {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low == len(s.blocks) {
		return low - 1
	}
	return low
}

func (s *gapSet) predecessor(blockIndex, index int) (utils.ByteInterval, bool) {
	if index > 0 {
		return s.blocks[blockIndex].gaps[index-1], true
	}
	if blockIndex > 0 {
		return s.blocks[blockIndex-1].last(), true
	}
	return utils.ByteInterval{}, false
}

func (s *gapSet) successor(blockIndex, index int) (utils.ByteInterval, bool) {
	block := s.blocks[blockIndex]
	if index+1 < block.count {
		return block.gaps[index+1], true
	}
	if blockIndex+1 < len(s.blocks) {
		return s.blocks[blockIndex+1].first(), true
	}
	return utils.ByteInterval{}, false
}

func (s *gapSet) mergeSparseBlock(blockIndex int) {
	block := s.blocks[blockIndex]
	if block.count >= gapBlockSplit {
		s.hint = blockIndex
		return
	}
	if blockIndex+1 < len(s.blocks) {
		next := s.blocks[blockIndex+1]
		if block.count+next.count <= gapBlockCapacity {
			copy(block.gaps[block.count:], next.values())
			block.count += next.count
			s.removeBlock(blockIndex + 1)
			s.hint = blockIndex
			return
		}
	}
	if blockIndex > 0 {
		previous := s.blocks[blockIndex-1]
		if previous.count+block.count <= gapBlockCapacity {
			copy(previous.gaps[previous.count:], block.values())
			previous.count += block.count
			s.removeBlock(blockIndex)
			s.hint = blockIndex - 1
			return
		}
	}
	s.hint = blockIndex
}

func (s *gapSet) removeBlock(index int) {
	copy(s.blocks[index:], s.blocks[index+1:])
	last := len(s.blocks) - 1
	s.blocks[last] = nil
	s.blocks = s.blocks[:last]
	if len(s.blocks) == 0 {
		s.hint = -1
		return
	}
	if index == len(s.blocks) {
		index--
	}
	s.hint = index
}
