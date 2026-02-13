package consensus

import (
	"testing"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

func TestSHA256HasherImplementsHasher(t *testing.T) {
	var _ Hasher = (*SHA256Hasher)(nil)
}

func TestSHA256HasherDeterministic(t *testing.T) {
	h := NewSHA256Hasher()
	defer h.Close()

	input := []byte("chronodrachma test input")
	hash1, err := h.Hash(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hash2, err := h.Hash(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("same input produced different hashes: %s vs %s", hash1.Hex(), hash2.Hex())
	}
}

func TestMeetsDifficulty_Zero(t *testing.T) {
	// difficulty=0 should accept any hash.
	h := types.Hash{0xFF, 0xFF, 0xFF}
	if !MeetsDifficulty(h, 0) {
		t.Fatal("difficulty=0 should accept any hash")
	}
}

func TestMeetsDifficulty_High(t *testing.T) {
	// An all-0xFF hash should fail any nonzero difficulty.
	h := types.Hash{}
	for i := range h {
		h[i] = 0xFF
	}
	if MeetsDifficulty(h, 1) {
		t.Fatal("all-0xFF hash should fail difficulty=1")
	}
}

func TestMeetsDifficulty_LeadingZeros(t *testing.T) {
	tests := []struct {
		name       string
		hash       types.Hash
		difficulty uint64
		want       bool
	}{
		{
			name:       "8 zero bits, first byte 0x00",
			hash:       types.Hash{0x00, 0x80},
			difficulty: 8,
			want:       true,
		},
		{
			name:       "8 zero bits needed, first byte 0x01",
			hash:       types.Hash{0x01},
			difficulty: 8,
			want:       false,
		},
		{
			name:       "4 zero bits, first nibble 0x0",
			hash:       types.Hash{0x0F},
			difficulty: 4,
			want:       true,
		},
		{
			name:       "4 zero bits needed, first nibble 0x1",
			hash:       types.Hash{0x10},
			difficulty: 4,
			want:       false,
		},
		{
			name:       "16 zero bits",
			hash:       types.Hash{0x00, 0x00, 0x01},
			difficulty: 16,
			want:       true,
		},
		{
			name:       "all zeros passes max difficulty",
			hash:       types.Hash{},
			difficulty: 256,
			want:       true,
		},
		{
			name:       "difficulty > 256 always fails",
			hash:       types.Hash{},
			difficulty: 257,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MeetsDifficulty(tt.hash, tt.difficulty)
			if got != tt.want {
				t.Errorf("MeetsDifficulty(%x, %d) = %v, want %v", tt.hash[:4], tt.difficulty, got, tt.want)
			}
		})
	}
}
