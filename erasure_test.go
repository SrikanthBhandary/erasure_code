// Tests for Reed-Solomon erasure coding.
//
// Run with:  go test ./... -v
// Benchmark: go test ./... -bench=. -benchmem

package erasure

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// ── GF(2^8) arithmetic tests ──────────────────────────────────────────────────

func TestGFTables(t *testing.T) {
	// α^0 = 1
	if gfExp[0] != 1 {
		t.Errorf("gfExp[0] = %d, want 1", gfExp[0])
	}
	// log(1) = 0
	if gfLog[1] != 0 {
		t.Errorf("gfLog[1] = %d, want 0", gfLog[1])
	}
}

func TestGFMulCommutative(t *testing.T) {
	// Multiplication must be commutative: a*b == b*a
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if gfMul(byte(a), byte(b)) != gfMul(byte(b), byte(a)) {
				t.Fatalf("gfMul(%d,%d) != gfMul(%d,%d)", a, b, b, a)
			}
		}
	}
}

func TestGFMulIdentity(t *testing.T) {
	// a * 1 == a
	for a := 0; a < 256; a++ {
		if gfMul(byte(a), 1) != byte(a) {
			t.Errorf("gfMul(%d, 1) = %d, want %d", a, gfMul(byte(a), 1), a)
		}
	}
}

func TestGFMulZero(t *testing.T) {
	// a * 0 == 0
	for a := 0; a < 256; a++ {
		if gfMul(byte(a), 0) != 0 {
			t.Errorf("gfMul(%d, 0) != 0", a)
		}
	}
}

func TestGFDivInverse(t *testing.T) {
	// a / a == 1 (for a != 0)
	for a := 1; a < 256; a++ {
		if gfDiv(byte(a), byte(a)) != 1 {
			t.Errorf("gfDiv(%d,%d) = %d, want 1", a, a, gfDiv(byte(a), byte(a)))
		}
	}
}

func TestGFMulDivRoundtrip(t *testing.T) {
	// (a * b) / b == a
	for a := 1; a < 256; a++ {
		for b := 1; b < 256; b++ {
			product := gfMul(byte(a), byte(b))
			quotient := gfDiv(product, byte(b))
			if quotient != byte(a) {
				t.Fatalf("(%d*%d)/%d = %d, want %d", a, b, b, quotient, a)
			}
		}
	}
}

// ── Matrix inversion tests ────────────────────────────────────────────────────

func TestMatInverse2x2(t *testing.T) {
	// Build a simple 2x2 identity and verify its inverse is itself
	m := Matrix{
		{1, 0},
		{0, 1},
	}
	inv, err := matInverse(m)
	if err != nil {
		t.Fatal(err)
	}
	// Identity inverse is identity
	if inv[0][0] != 1 || inv[1][1] != 1 || inv[0][1] != 0 || inv[1][0] != 0 {
		t.Errorf("inverse of identity is not identity: %v", inv)
	}
}

func TestMatInverseRoundtrip(t *testing.T) {
	// M * M^(-1) should be identity
	m := Matrix{
		{3, 2},
		{5, 7},
	}
	inv, err := matInverse(m)
	if err != nil {
		t.Fatal(err)
	}
	n := len(m)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			var acc byte
			for k := 0; k < n; k++ {
				acc ^= gfMul(m[i][k], inv[k][j])
			}
			want := byte(0)
			if i == j {
				want = 1
			}
			if acc != want {
				t.Errorf("M*M^-1[%d][%d] = %d, want %d", i, j, acc, want)
			}
		}
	}
}

// ── Encoder: basic encode + decode ───────────────────────────────────────────

func TestEncodeDecodeNoLoss(t *testing.T) {
	// With no shard loss, reconstruct must return original data
	enc, err := NewEncoder(4, 2)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("Hello, Erasure Coding World!1234") // 32 bytes, divisible by 4

	shards, err := enc.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 6 {
		t.Fatalf("expected 6 shards, got %d", len(shards))
	}

	// Pass all shards (no loss)
	recovered, err := enc.Reconstruct(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, original) {
		t.Errorf("recovered != original\ngot:  %q\nwant: %q", recovered, original)
	}
}

func TestSystematicProperty(t *testing.T) {
	// In a systematic code, the first K shards ARE the original data blocks.
	enc, err := NewEncoder(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("ABCDEFGHIJKLMNO") // 15 bytes, divisible by 3 → 5 bytes/shard

	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatal(err)
	}

	// Shard 0 = "ABCDE", shard 1 = "FGHIJ", shard 2 = "KLMNO"
	if !bytes.Equal(shards[0], []byte("ABCDE")) {
		t.Errorf("shard[0] = %q, want 'ABCDE'", shards[0])
	}
	if !bytes.Equal(shards[1], []byte("FGHIJ")) {
		t.Errorf("shard[1] = %q, want 'FGHIJ'", shards[1])
	}
	if !bytes.Equal(shards[2], []byte("KLMNO")) {
		t.Errorf("shard[2] = %q, want 'KLMNO'", shards[2])
	}
}

// ── Fault tolerance tests ─────────────────────────────────────────────────────

// dropShards returns a copy of shards with the given indices set to nil.
func dropShards(shards [][]byte, drop ...int) [][]byte {
	out := make([][]byte, len(shards))
	copy(out, shards)
	for _, i := range drop {
		out[i] = nil
	}
	return out
}

func TestReconstructWithOneLoss(t *testing.T) {
	enc, _ := NewEncoder(4, 2) // tolerate up to 2 losses
	data := makeData(4, 8)     // 32 bytes

	shards, _ := enc.Encode(data)

	// Try losing each single shard
	for drop := 0; drop < 6; drop++ {
		sparse := dropShards(shards, drop)
		recovered, err := enc.Reconstruct(sparse)
		if err != nil {
			t.Errorf("drop shard %d: reconstruct failed: %v", drop, err)
			continue
		}
		if !bytes.Equal(recovered, data) {
			t.Errorf("drop shard %d: recovered != original", drop)
		}
	}
}

func TestReconstructWithTwoLosses(t *testing.T) {
	enc, _ := NewEncoder(4, 2) // exactly 2 parity shards
	data := makeData(4, 8)

	shards, _ := enc.Encode(data)

	// Try every combination of 2 lost shards
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			sparse := dropShards(shards, i, j)
			recovered, err := enc.Reconstruct(sparse)
			if err != nil {
				t.Errorf("drop shards {%d,%d}: %v", i, j, err)
				continue
			}
			if !bytes.Equal(recovered, data) {
				t.Errorf("drop shards {%d,%d}: data mismatch", i, j)
			}
		}
	}
}

func TestReconstructTooManyLosses(t *testing.T) {
	enc, _ := NewEncoder(4, 2) // can only tolerate 2 losses
	data := makeData(4, 8)
	shards, _ := enc.Encode(data)

	// Lose 3 shards → should fail
	sparse := dropShards(shards, 0, 1, 2)
	_, err := enc.Reconstruct(sparse)
	if err == nil {
		t.Error("expected error with 3 lost shards (only 2 parity), got nil")
	} else {
		t.Logf("correctly rejected: %v", err)
	}
}

func TestReconstructParityShardsOnly(t *testing.T) {
	// If all data shards are lost but enough parity shards remain → still works
	enc, _ := NewEncoder(2, 4) // 2 data + 4 parity = tolerate 4 losses
	data := []byte("HELLO!!!") // 8 bytes / 2 = 4 bytes per shard

	shards, _ := enc.Encode(data)

	// Lose both data shards (indices 0, 1) — rely entirely on parity
	sparse := dropShards(shards, 0, 1)
	recovered, err := enc.Reconstruct(sparse)
	if err != nil {
		t.Fatalf("failed to reconstruct from parity only: %v", err)
	}
	if !bytes.Equal(recovered, data) {
		t.Errorf("parity-only recovery failed: got %q, want %q", recovered, data)
	}
}

// ── Table-driven: multiple (K, M) configs ────────────────────────────────────

func TestVariousConfigs(t *testing.T) {
	configs := []struct {
		k, m int
	}{
		{1, 1},
		{2, 1},
		{3, 2},
		{4, 4},
		{6, 3},
		{10, 4},
	}

	for _, cfg := range configs {
		t.Run(fmt.Sprintf("K%d_M%d", cfg.k, cfg.m), func(t *testing.T) {
			enc, err := NewEncoder(cfg.k, cfg.m)
			if err != nil {
				t.Fatal(err)
			}
			data := makeData(cfg.k, 16)
			shards, err := enc.Encode(data)
			if err != nil {
				t.Fatal(err)
			}

			// Drop exactly cfg.m shards (worst case = maximum allowed loss)
			rng := rand.New(rand.NewSource(42))
			perm := rng.Perm(cfg.k + cfg.m)
			toDrop := perm[:cfg.m]
			sparse := dropShards(shards, toDrop...)

			recovered, err := enc.Reconstruct(sparse)
			if err != nil {
				t.Fatalf("reconstruct failed after dropping %v: %v", toDrop, err)
			}
			if !bytes.Equal(recovered, data) {
				t.Error("data mismatch after reconstruction")
			}
		})
	}
}

// ── Edge cases ────────────────────────────────────────────────────────────────

func TestEncoderValidation(t *testing.T) {
	_, err := NewEncoder(0, 2)
	if err == nil {
		t.Error("expected error for dataShards=0")
	}
	_, err = NewEncoder(2, 0)
	if err == nil {
		t.Error("expected error for parityShards=0")
	}
	_, err = NewEncoder(200, 60)
	if err == nil {
		t.Error("expected error for total > 255")
	}
}

func TestEncodeDataNotDivisible(t *testing.T) {
	enc, _ := NewEncoder(4, 2)
	_, err := enc.Encode([]byte("not divisible")) // 13 bytes, not divisible by 4
	if err == nil {
		t.Error("expected error for data not divisible by dataShards")
	}
}

func TestSingleBytePerShard(t *testing.T) {
	enc, _ := NewEncoder(3, 2)
	data := []byte{10, 20, 30} // 1 byte per shard
	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatal(err)
	}
	sparse := dropShards(shards, 0, 1) // lose 2 data shards
	recovered, err := enc.Reconstruct(sparse)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, data) {
		t.Errorf("single-byte shard recovery failed: got %v, want %v", recovered, data)
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

// BenchmarkEncode measures encoding throughput.
func BenchmarkEncode(b *testing.B) {
	enc, _ := NewEncoder(8, 4)
	data := makeData(8, 1024) // 8 KB total
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(data) //nolint
	}
}

// BenchmarkReconstruct measures reconstruction throughput.
func BenchmarkReconstruct(b *testing.B) {
	enc, _ := NewEncoder(8, 4)
	data := makeData(8, 1024)
	shards, _ := enc.Encode(data)
	// Pre-drop 4 shards
	sparse := dropShards(shards, 0, 2, 5, 8)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Reconstruct(sparse) //nolint
	}
}

// ── Helper ────────────────────────────────────────────────────────────────────

// makeData creates deterministic test data: k*bytesPerShard bytes.
func makeData(k, bytesPerShard int) []byte {
	data := make([]byte, k*bytesPerShard)
	for i := range data {
		data[i] = byte(i*7 + 13)
	}
	return data
}
