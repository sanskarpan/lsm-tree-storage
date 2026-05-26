package bloom

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBloom_NoFalseNegatives(t *testing.T) {
	var keys [][]byte
	for i := 0; i < 10000; i++ {
		keys = append(keys, []byte(fmt.Sprintf("key-%06d", i)))
	}
	bf := NewBloomFilter(keys, 10)
	for _, key := range keys {
		assert.True(t, bf.MayContain(key), "must not have false negatives: %s", key)
	}
}

func TestBloom_FalsePositiveRate(t *testing.T) {
	var keys [][]byte
	for i := 0; i < 10000; i++ {
		keys = append(keys, []byte(fmt.Sprintf("key-%06d", i)))
	}
	bf := NewBloomFilter(keys, 10)
	fp := 0
	for i := 10000; i < 20000; i++ {
		if bf.MayContain([]byte(fmt.Sprintf("key-%06d", i))) {
			fp++
		}
	}
	fpr := float64(fp) / 10000.0
	assert.Less(t, fpr, 0.02, "FP rate must be < 2%% for bpk=10, got %.4f", fpr)
}

func TestBloom_EmptyFilter(t *testing.T) {
	bf := NewBloomFilter(nil, 10)
	assert.False(t, bf.MayContain([]byte("anything")))
}

func TestBloom_SerializeDeserialize(t *testing.T) {
	var keys [][]byte
	for i := 0; i < 1000; i++ {
		keys = append(keys, []byte(fmt.Sprintf("k%d", i)))
	}
	bf := NewBloomFilter(keys, 10)

	data := bf.Serialize()
	bf2 := DeserializeBloomFilter(data)

	// All inserted keys must still be found
	for _, key := range keys {
		assert.True(t, bf2.MayContain(key), "after deserialize: false negative for %s", key)
	}

	// Results must match original
	for i := 1000; i < 2000; i++ {
		key := []byte(fmt.Sprintf("k%d", i))
		assert.Equal(t, bf.MayContain(key), bf2.MayContain(key))
	}
}

func TestBloom_BitsPerKeyScaling(t *testing.T) {
	var keys [][]byte
	for i := 0; i < 10000; i++ {
		keys = append(keys, []byte(fmt.Sprintf("key-%06d", i)))
	}

	for _, tc := range []struct {
		bpk    int
		maxFPR float64
	}{
		{6, 0.10},
		{10, 0.02},
		{14, 0.005},
	} {
		bf := NewBloomFilter(keys, tc.bpk)
		fp := 0
		for i := 10000; i < 20000; i++ {
			if bf.MayContain([]byte(fmt.Sprintf("key-%06d", i))) {
				fp++
			}
		}
		fpr := float64(fp) / 10000.0
		assert.Less(t, fpr, tc.maxFPR, "bpk=%d: FP rate %.4f exceeds %.4f", tc.bpk, fpr, tc.maxFPR)
		// Ensure no false negatives
		for _, key := range keys {
			assert.True(t, bf.MayContain(key))
		}
	}
}
