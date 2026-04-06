package runtime

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"strings"
)

const defaultLocalStubEmbeddingDimensions = 16

type localStubEmbedder struct {
	dimensions int
}

func newLocalStubEmbedder(dimensions int) localStubEmbedder {
	if dimensions < 1 {
		dimensions = defaultLocalStubEmbeddingDimensions
	}
	return localStubEmbedder{dimensions: dimensions}
}

func (e localStubEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	embedding := make([]float32, e.dimensions)
	normalized := strings.TrimSpace(strings.ToLower(text))
	if normalized == "" {
		return embedding, nil
	}

	tokens := strings.Fields(normalized)
	for _, token := range tokens {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		sum := hasher.Sum64()
		slot := int(sum % uint64(e.dimensions))
		embedding[slot] += 1

		secondary := fnv.New64()
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], sum)
		_, _ = secondary.Write(buf[:])
		secondarySlot := int(secondary.Sum64() % uint64(e.dimensions))
		embedding[secondarySlot] += 0.25
	}

	return embedding, nil
}

func (e localStubEmbedder) EmbeddingIdentity() string {
	return "local_stub/" + itoa(e.dimensions)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	negative := value < 0
	if negative {
		value = -value
	}

	var digits [20]byte
	pos := len(digits)
	for value > 0 {
		pos--
		digits[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		digits[pos] = '-'
	}
	return string(digits[pos:])
}
