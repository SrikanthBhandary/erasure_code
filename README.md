# Reed-Solomon Erasure Coding in Go

A from-scratch implementation of Reed-Solomon erasure coding over GF(2^8).
Split any data into N shards — lose any M, recover perfectly from the rest.

## Project Structure

```
erasure_code/
├── erasure.go        # Core: GF(2^8) math, Cauchy matrix, Encoder
├── erasure_test.go   # Tests: GF arithmetic, encode/decode, fault tolerance
├── utf8.go           # UTF-8 / arbitrary bytes safe wrapper (padding + header)
├── utf8_test.go      # Tests: multilingual strings, emoji, edge cases
├── file.go           # File-level encoder with SHA-256 integrity check
├── file_test.go      # Tests: MP3/binary files, corruption detection
├── go.mod
└── demo/
    └── main.go       # Interactive demo showing all features
```

## Quick Start

```bash
# Run tests
go test ./... -v

# Run benchmarks
go test ./... -bench=. -benchmem

# Run interactive demo
go run demo/main.go
```

## Usage

### Basic encoding (raw bytes, must be divisible by dataShards)
```go
enc, _ := erasure.NewEncoder(4, 2)          // 4 data + 2 parity = tolerate 2 losses
shards, _ := enc.Encode(data)               // split into 6 shards
shards[0] = nil                             // simulate losing shard 0
shards[3] = nil                             // simulate losing shard 3
recovered, _ := enc.Reconstruct(shards)    // recover from any 4 survivors
```

### UTF-8 strings and arbitrary bytes (recommended)
```go
enc, _ := erasure.NewSafeEncoder(4, 2)
shards, _ := enc.EncodeString("Hello 🔥 世界")   // any UTF-8, any length
shards[1] = nil
text, _ := enc.ReconstructString(shards)
```

### Files (MP3, PDF, images, anything)
```go
fe, _ := erasure.NewFileEncoder(4, 2)
fe.EncodeFile("song.mp3")
// creates: song.mp3.shard.0 ... song.mp3.shard.5

fe.ReconstructFile([]string{
    "song.mp3.shard.1",
    "song.mp3.shard.2",
    "song.mp3.shard.3",
    "song.mp3.shard.5",
}, "song_recovered.mp3")
// SHA-256 verified — byte-for-byte identical to original
```

## How It Works

### Layer 1 — GF(2^8) arithmetic
All math happens in Galois Field GF(2^8) — integers 0-255 where:
- **Addition = XOR** (no carry, no overflow, every number cancels itself)
- **Multiplication** = log/anti-log table lookup (always stays 0-255)
- **Division** = multiply by inverse (exact integer, no fractions ever)

### Layer 2 — Cauchy encoding matrix
The encoding matrix is structured as:
```
Top K rows    = Identity matrix   → data shards == original blocks (systematic)
Bottom M rows = Cauchy matrix     → parity shards = mixed fingerprints

Cauchy[i][j] = 1 / (x[i] XOR y[j])   where x and y are disjoint sets
```
**Key property:** Any K×K sub-matrix of a Cauchy matrix is always invertible.
This guarantees reconstruction always works regardless of which shards survived.

### Layer 3 — Reconstruction
```
received_shards = sub_matrix × original_data
original_data   = inv(sub_matrix) × received_shards
```
Pick K surviving shards → extract their rows → invert the K×K sub-matrix via
Gauss-Jordan elimination → multiply by received data → original bytes recovered.

### Layer 4 — Padding (utf8.go)
Arbitrary-length data is wrapped with a 4-byte Big Endian length header and
zero-padded to the next multiple of dataShards before encoding. The header
allows exact trimming after reconstruction.

### Layer 5 — File integrity (file.go)
Each shard file carries a 64-byte header including the SHA-256 hash of the
original file. After reconstruction the hash is recomputed and verified —
silent corruption is impossible to miss.

## Configuration Guide

| Config  | Overhead | Tolerates | Use case                        |
|---------|----------|-----------|---------------------------------|
| 4 + 2   | 1.5×     | 2 losses  | General purpose                 |
| 6 + 3   | 1.5×     | 3 losses  | Higher redundancy               |
| 8 + 4   | 1.5×     | 4 losses  | Production storage              |
| 2 + 4   | 3×       | 4 losses  | Maximum fault tolerance         |
| 10 + 4  | 1.4×     | 4 losses  | Large files, low overhead       |

**Limit:** dataShards + parityShards ≤ 128 (Cauchy construction constraint)

## Real-World Uses of This Algorithm
- Amazon S3, Google Cloud Storage, Backblaze B2
- HDFS (Hadoop Distributed File System)
- Blu-ray discs and DVDs (scratch recovery)
- QR codes (partial occlusion recovery)
- Voyager space probe (deep space transmission errors)
- RAID-6 storage arrays
