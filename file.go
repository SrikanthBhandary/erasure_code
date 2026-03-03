// file.go — File-level erasure coding (MP3, PDF, images, any binary file).
//
// Works identically to the UTF-8 wrapper but:
//   1. Reads/writes actual files from disk
//   2. Saves each shard as a separate file  (filename.shard.0, .1, .2 ...)
//   3. Reconstructs the original file from any K shard files
//   4. Verifies integrity with a SHA-256 checksum baked into the header

package erasure

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
)

// FileEncoder wraps SafeEncoder with file I/O.
type FileEncoder struct {
	safe *SafeEncoder
}

// NewFileEncoder creates an encoder for files.
func NewFileEncoder(dataShards, parityShards int) (*FileEncoder, error) {
	safe, err := NewSafeEncoder(dataShards, parityShards)
	if err != nil {
		return nil, err
	}
	return &FileEncoder{safe: safe}, nil
}

// ShardInfo describes one shard file written to disk.
type ShardInfo struct {
	Index int
	Path  string
	Size  int
}

// EncodeFile reads a file, encodes it, and writes N shard files next to it.
//
// Output files: <originalPath>.shard.0, .shard.1, ... .shard.N-1
// Each shard file has a small binary header:
//
//	[4]  magic       "ERSC"
//	[4]  version     0x00000001
//	[4]  shardIndex
//	[4]  dataShards
//	[4]  parityShards
//	[4]  shardDataLen  (bytes of shard payload in this file)
//	[8]  originalLen   (original file size in bytes)
//	[32] sha256        (checksum of original file)
//	[N]  shard payload
func (fe *FileEncoder) EncodeFile(path string) ([]ShardInfo, error) {
	// Read original file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Compute checksum of original
	checksum := sha256.Sum256(data)

	// Encode using SafeEncoder (handles padding)
	shards, err := fe.safe.EncodeBytes(data)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	k := fe.safe.enc.DataShards()
	m := fe.safe.enc.ParityShards()
	infos := make([]ShardInfo, len(shards))

	for i, shard := range shards {
		shardPath := fmt.Sprintf("%s.shard.%d", path, i)

		f, err := os.Create(shardPath)
		if err != nil {
			return nil, fmt.Errorf("create shard %d: %w", i, err)
		}

		// Write header
		writeUint32 := func(v uint32) { binary.Write(f, binary.BigEndian, v) }
		writeUint64 := func(v uint64) { binary.Write(f, binary.BigEndian, v) }

		f.Write([]byte("ERSC"))         // magic
		writeUint32(1)                  // version
		writeUint32(uint32(i))          // shard index
		writeUint32(uint32(k))          // data shards
		writeUint32(uint32(m))          // parity shards
		writeUint32(uint32(len(shard))) // shard payload length
		writeUint64(uint64(len(data)))  // original file size
		f.Write(checksum[:])            // sha256 checksum (32 bytes)

		// Write shard payload
		f.Write(shard)
		f.Close()

		infos[i] = ShardInfo{Index: i, Path: shardPath, Size: len(shard)}
	}
	return infos, nil
}

// ReconstructFile reconstructs the original file from shard files.
// Pass any K or more shard file paths — extras are ignored.
// The output file is written to outputPath.
func (fe *FileEncoder) ReconstructFile(shardPaths []string, outputPath string) error {
	// Read and parse shard files
	type shardData struct {
		index        int
		dataShards   int
		parityShards int
		originalLen  uint64
		checksum     [32]byte
		payload      []byte
	}

	parsed := make([]shardData, 0, len(shardPaths))
	for _, p := range shardPaths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read shard %q: %w", p, err)
		}
		if len(raw) < 60 {
			return fmt.Errorf("shard %q too small to be valid", p)
		}
		magic := string(raw[:4])
		if magic != "ERSC" {
			return fmt.Errorf("shard %q has wrong magic %q", p, magic)
		}

		var sd shardData
		sd.index = int(binary.BigEndian.Uint32(raw[8:12]))
		sd.dataShards = int(binary.BigEndian.Uint32(raw[12:16]))
		sd.parityShards = int(binary.BigEndian.Uint32(raw[16:20]))
		payloadLen := int(binary.BigEndian.Uint32(raw[20:24]))
		sd.originalLen = binary.BigEndian.Uint64(raw[24:32])
		copy(sd.checksum[:], raw[32:64])
		sd.payload = raw[64 : 64+payloadLen]
		parsed = append(parsed, sd)
	}

	if len(parsed) == 0 {
		return fmt.Errorf("no valid shard files provided")
	}

	// Use metadata from first shard
	meta := parsed[0]
	total := meta.dataShards + meta.parityShards

	// Reconstruct encoder with same parameters
	enc, err := NewEncoder(meta.dataShards, meta.parityShards)
	if err != nil {
		return fmt.Errorf("create encoder: %w", err)
	}

	// Build sparse shard slice
	shards := make([][]byte, total)
	for _, sd := range parsed {
		if sd.index < total {
			shards[sd.index] = sd.payload
		}
	}

	// Reconstruct padded bytes
	padded, err := enc.Reconstruct(shards)
	if err != nil {
		return fmt.Errorf("reconstruct: %w", err)
	}

	// Unpad to get original bytes
	original, err := unpad(padded)
	if err != nil {
		return fmt.Errorf("unpad: %w", err)
	}

	// Verify checksum
	got := sha256.Sum256(original)
	if got != meta.checksum {
		return fmt.Errorf("SHA-256 mismatch — file is corrupted!\n  got:  %x\n  want: %x",
			got, meta.checksum)
	}

	// Write output
	if err := os.WriteFile(outputPath, original, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
