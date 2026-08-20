package quic

import "github.com/apernet/quic-go/internal/utils"

const (
	gapInlineCapacity = 8
	gapBlockCapacity  = 128
	gapBlockSplit     = gapBlockCapacity / 2
)

type gapBlock struct {
	count int
	gaps  [gapBlockCapacity]utils.ByteInterval
}

func (b *gapBlock) values() []utils.ByteInterval {
	return b.gaps[:b.count]
}

func (b *gapBlock) first() utils.ByteInterval {
	return b.gaps[0]
}

func (b *gapBlock) last() utils.ByteInterval {
	return b.gaps[b.count-1]
}

// gapSet stores disjoint intervals ordered by Start, which also makes End monotonic.
// Small sets stay inline. Larger sets use dense blocks to reduce allocation and lookup costs.
type gapSet struct {
	length int
	hint   int
	inline [gapInlineCapacity]utils.ByteInterval
	blocks []*gapBlock
}

func (s *gapSet) Len() int {
	return s.length
}

func (s *gapSet) MatchInto(cond utils.ByteInterval, dst []utils.ByteInterval) []utils.ByteInterval {
	if s.blocks != nil {
		return s.matchBlocks(cond, dst)
	}
	for index := 0; index < s.length; index++ {
		gap := s.inline[index]
		if gap.End < cond.Start {
			continue
		}
		if gap.Start > cond.End {
			break
		}
		dst = append(dst, gap)
	}
	return dst
}

func (s *gapSet) Insert(value utils.ByteInterval) {
	if s.blocks != nil {
		s.insertBlock(value)
		return
	}
	index := gapLowerBound(s.inline[:s.length], value)
	if index < s.length && value.Comp(s.inline[index]) == 0 {
		s.inline[index] = value
		return
	}
	if s.length == gapInlineCapacity {
		s.promote()
		s.insertBlock(value)
		return
	}
	copy(s.inline[index+1:s.length+1], s.inline[index:s.length])
	s.inline[index] = value
	s.length++
}

func (s *gapSet) Delete(value utils.ByteInterval) {
	if s.blocks != nil {
		s.deleteBlock(value)
		return
	}
	index := gapLowerBound(s.inline[:s.length], value)
	if index == s.length || value.Comp(s.inline[index]) != 0 {
		return
	}
	copy(s.inline[index:s.length-1], s.inline[index+1:s.length])
	s.length--
	s.inline[s.length] = utils.ByteInterval{}
}

func (s *gapSet) UpdatePreservingOrder(oldValue, newValue utils.ByteInterval) bool {
	if s.blocks != nil {
		return s.updateBlock(oldValue, newValue)
	}
	index := gapLowerBound(s.inline[:s.length], oldValue)
	if index == s.length || oldValue.Comp(s.inline[index]) != 0 {
		return false
	}
	direction := newValue.Comp(oldValue)
	if direction < 0 && index > 0 && newValue.Comp(s.inline[index-1]) <= 0 {
		return false
	}
	if direction > 0 && index+1 < s.length && newValue.Comp(s.inline[index+1]) >= 0 {
		return false
	}
	s.inline[index] = newValue
	return true
}

func (s *gapSet) promote() {
	block := new(gapBlock)
	block.count = s.length
	copy(block.gaps[:], s.inline[:s.length])
	s.inline = [gapInlineCapacity]utils.ByteInterval{}
	s.blocks = []*gapBlock{block}
	s.hint = 0
}

func (s *gapSet) demote() {
	var inline [gapInlineCapacity]utils.ByteInterval
	position := 0
	for _, block := range s.blocks {
		position += copy(inline[position:], block.values())
	}
	s.inline = inline
	s.blocks = nil
	s.hint = -1
}

func gapLowerBound(values []utils.ByteInterval, target utils.ByteInterval) int {
	low, high := 0, len(values)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if values[middle].Comp(target) < 0 {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}
