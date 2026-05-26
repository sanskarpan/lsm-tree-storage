// Package bloom provides a LevelDB-compatible Bloom filter for probabilistic
// key membership tests used by SSTable readers.
package bloom

import (
	"encoding/binary"
	"math"
)

// bloomHash is LevelDB-style murmur-inspired hash
func bloomHash(key []byte) uint32 {
	const (
		seed = uint32(0xbc9f1d34)
		m    = uint32(0xc6a4a793)
	)
	h := seed ^ uint32(len(key))*m
	for ; len(key) >= 4; key = key[4:] {
		w := binary.LittleEndian.Uint32(key)
		h += w
		h *= m
		h ^= h >> 16
	}
	switch len(key) {
	case 3:
		h += uint32(key[2]) << 16
		fallthrough
	case 2:
		h += uint32(key[1]) << 8
		fallthrough
	case 1:
		h += uint32(key[0])
		h *= m
		h ^= h >> 24
	}
	return h
}

// BloomFilter is a probabilistic set membership data structure
type BloomFilter struct {
	bits []byte
}

// NewBloomFilter creates a Bloom filter for keys with bitsPerKey bits per key
func NewBloomFilter(keys [][]byte, bitsPerKey int) *BloomFilter {
	if bitsPerKey < 1 {
		bitsPerKey = 1
	}
	k := int(float64(bitsPerKey) * math.Ln2)
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	nBits := len(keys) * bitsPerKey
	nBits = (nBits + 7) &^ 7 // round up to byte boundary
	if nBits < 64 {
		nBits = 64
	}

	bits := make([]byte, nBits/8+1) // +1 for k value at end
	for _, key := range keys {
		h := bloomHash(key)
		delta := (h >> 17) | (h << 15) // rotl by 15
		for j := 0; j < k; j++ {
			bitPos := uint32(h) % uint32(nBits)
			bits[bitPos/8] |= 1 << (bitPos % 8)
			h += delta
		}
	}
	bits[len(bits)-1] = byte(k) // store k as last byte (LevelDB convention)
	return &BloomFilter{bits: bits}
}

// MayContain returns true if key might be in the set (never false-negatives)
func (f *BloomFilter) MayContain(key []byte) bool {
	if len(f.bits) < 2 {
		return false
	}
	k := int(f.bits[len(f.bits)-1])
	if k > 30 {
		return true // unknown filter — be conservative
	}
	nBits := (len(f.bits) - 1) * 8

	h := bloomHash(key)
	delta := (h >> 17) | (h << 15)
	for j := 0; j < k; j++ {
		bitPos := uint32(h) % uint32(nBits)
		if f.bits[bitPos/8]&(1<<(bitPos%8)) == 0 {
			return false // definitely not in set
		}
		h += delta
	}
	return true // probably in set
}

// Serialize returns the raw bytes of the filter (including the k byte)
func (f *BloomFilter) Serialize() []byte {
	result := make([]byte, len(f.bits))
	copy(result, f.bits)
	return result
}

// DeserializeBloomFilter deserializes a filter from raw bytes
func DeserializeBloomFilter(data []byte) *BloomFilter {
	bits := make([]byte, len(data))
	copy(bits, data)
	return &BloomFilter{bits: bits}
}

// NumHashFuncs returns the number of hash functions (k)
func (f *BloomFilter) NumHashFuncs() int {
	if len(f.bits) == 0 {
		return 0
	}
	return int(f.bits[len(f.bits)-1])
}

// BitCount returns the number of bit positions in the filter
func (f *BloomFilter) BitCount() int {
	if len(f.bits) < 2 {
		return 0
	}
	return (len(f.bits) - 1) * 8
}
