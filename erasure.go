// Package erasure implements Reed-Solomon erasure coding over GF(2^8).
//
// Uses a CAUCHY matrix which guarantees any square sub-matrix is invertible,
// making reconstruction always possible as long as K shards are available.

package erasure

import "fmt"

// ── GF(2^8) arithmetic ────────────────────────────────────────────────────────

const gfPoly = 0x11d

var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&256 != 0 {
			x ^= gfPoly
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func gfInv(a byte) byte {
	if a == 0 {
		panic("inverse of zero in GF(2^8)")
	}
	return gfExp[255-int(gfLog[a])]
}

func gfDiv(a, b byte) byte {
	return gfMul(a, gfInv(b))
}

func gfPow(a byte, n int) byte {
	if n == 0 {
		return 1
	}
	return gfExp[(int(gfLog[a])*n)%255]
}

// ── Matrix ────────────────────────────────────────────────────────────────────

type Matrix [][]byte

func newMatrix(rows, cols int) Matrix {
	m := make(Matrix, rows)
	for i := range m {
		m[i] = make([]byte, cols)
	}
	return m
}

// buildEncMatrix returns a (totalShards x dataShards) encoding matrix:
//   rows 0..K-1  = identity  (systematic: shard i == input block i)
//   rows K..N-1  = Cauchy    (parity rows)
//
// Cauchy[i][j] = 1 / (x[i] XOR y[j])  where x and y are disjoint sets.
// Any square sub-matrix of a Cauchy matrix is invertible — this is the key
// property that guarantees Reed-Solomon reconstruction always works.
func buildEncMatrix(dataShards, parityShards int) Matrix {
	total := dataShards + parityShards
	m := newMatrix(total, dataShards)

	// Identity for data rows
	for i := 0; i < dataShards; i++ {
		m[i][i] = 1
	}

	// Cauchy parity rows
	// x[i] = i  (for parity rows, i in 0..M-1)
	// y[j] = M+j (for columns, j in 0..K-1)  — disjoint from x
	for i := 0; i < parityShards; i++ {
		for j := 0; j < dataShards; j++ {
			m[dataShards+i][j] = gfInv(byte(i) ^ byte(parityShards+j))
		}
	}
	return m
}

// matInverse computes the inverse of a square GF(2^8) matrix via Gauss-Jordan.
func matInverse(m Matrix) (Matrix, error) {
	n := len(m)
	aug := newMatrix(n, 2*n)
	for i := 0; i < n; i++ {
		copy(aug[i], m[i])
		aug[i][n+i] = 1
	}
	for col := 0; col < n; col++ {
		if aug[col][col] == 0 {
			found := false
			for row := col + 1; row < n; row++ {
				if aug[row][col] != 0 {
					aug[col], aug[row] = aug[row], aug[col]
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("matrix is singular")
			}
		}
		scale := gfInv(aug[col][col])
		for j := 0; j < 2*n; j++ {
			aug[col][j] = gfMul(aug[col][j], scale)
		}
		for row := 0; row < n; row++ {
			if row == col || aug[row][col] == 0 {
				continue
			}
			factor := aug[row][col]
			for j := 0; j < 2*n; j++ {
				aug[row][j] ^= gfMul(factor, aug[col][j])
			}
		}
	}
	inv := newMatrix(n, n)
	for i := 0; i < n; i++ {
		copy(inv[i], aug[i][n:])
	}
	return inv, nil
}

func matVecMul(m Matrix, v []byte) []byte {
	rows, cols := len(m), len(m[0])
	out := make([]byte, rows)
	for i := 0; i < rows; i++ {
		var acc byte
		for j := 0; j < cols; j++ {
			acc ^= gfMul(m[i][j], v[j])
		}
		out[i] = acc
	}
	return out
}

// ── Encoder ───────────────────────────────────────────────────────────────────

type Encoder struct {
	dataShards   int
	parityShards int
	totalShards  int
	encMatrix    Matrix
}

// NewEncoder creates a Reed-Solomon encoder.
// dataShards (K) + parityShards (M) must be ≤ 128.
func NewEncoder(dataShards, parityShards int) (*Encoder, error) {
	if dataShards <= 0 || parityShards <= 0 {
		return nil, fmt.Errorf("dataShards and parityShards must be > 0")
	}
	if dataShards+parityShards > 128 {
		return nil, fmt.Errorf("total shards must be ≤ 128")
	}
	return &Encoder{
		dataShards:   dataShards,
		parityShards: parityShards,
		totalShards:  dataShards + parityShards,
		encMatrix:    buildEncMatrix(dataShards, parityShards),
	}, nil
}

// DataShards returns the number of data shards K.
func (e *Encoder) DataShards() int { return e.dataShards }

// ParityShards returns the number of parity shards M.
func (e *Encoder) ParityShards() int { return e.parityShards }

// TotalShards returns total shards N = K + M.
func (e *Encoder) TotalShards() int { return e.totalShards }

// Encode splits data into dataShards blocks and produces totalShards shards.
// len(data) must be divisible by dataShards.
// The first K output shards are identical to the input blocks.
func (e *Encoder) Encode(data []byte) ([][]byte, error) {
	if len(data)%e.dataShards != 0 {
		return nil, fmt.Errorf("data length %d not divisible by dataShards %d",
			len(data), e.dataShards)
	}
	shardLen := len(data) / e.dataShards
	shards := make([][]byte, e.totalShards)
	for i := range shards {
		shards[i] = make([]byte, shardLen)
	}
	vec := make([]byte, e.dataShards)
	for pos := 0; pos < shardLen; pos++ {
		for i := 0; i < e.dataShards; i++ {
			vec[i] = data[i*shardLen+pos]
		}
		out := matVecMul(e.encMatrix, vec)
		for i := 0; i < e.totalShards; i++ {
			shards[i][pos] = out[i]
		}
	}
	return shards, nil
}

// Reconstruct recovers the original data from any K non-nil shards.
// Pass nil for lost shards.
func (e *Encoder) Reconstruct(shards [][]byte) ([]byte, error) {
	if len(shards) != e.totalShards {
		return nil, fmt.Errorf("expected %d shards, got %d", e.totalShards, len(shards))
	}
	present := make([]int, 0, e.dataShards)
	for i, s := range shards {
		if s != nil {
			present = append(present, i)
		}
	}
	if len(present) < e.dataShards {
		return nil, fmt.Errorf("need %d shards, only %d available", e.dataShards, len(present))
	}
	present = present[:e.dataShards]

	sub := newMatrix(e.dataShards, e.dataShards)
	for i, idx := range present {
		copy(sub[i], e.encMatrix[idx])
	}
	inv, err := matInverse(sub)
	if err != nil {
		return nil, fmt.Errorf("reconstruction failed: %w", err)
	}

	shardLen := len(shards[present[0]])
	result := make([]byte, e.dataShards*shardLen)
	vec := make([]byte, e.dataShards)
	for pos := 0; pos < shardLen; pos++ {
		for i, idx := range present {
			vec[i] = shards[idx][pos]
		}
		decoded := matVecMul(inv, vec)
		for i := 0; i < e.dataShards; i++ {
			result[i*shardLen+pos] = decoded[i]
		}
	}
	return result, nil
}
