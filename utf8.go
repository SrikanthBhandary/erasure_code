// utf8.go — UTF-8 safe wrapper around the core Encoder.
//
// Problem: our Encoder requires len(data) % dataShards == 0.
// UTF-8 strings are variable-length (1-4 bytes per character),
// so raw string bytes almost never satisfy this constraint.
//
// Solution:
//   1. Store original byte length in a 4-byte header
//   2. Pad with zeros to next multiple of dataShards
//   3. Encode the padded bytes
//   4. After reconstruction, strip padding using the header

package erasure

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// SafeEncoder wraps Encoder with UTF-8 / arbitrary-bytes support.
type SafeEncoder struct {
	enc *Encoder
}

// NewSafeEncoder creates a UTF-8 safe encoder.
func NewSafeEncoder(dataShards, parityShards int) (*SafeEncoder, error) {
	enc, err := NewEncoder(dataShards, parityShards)
	if err != nil {
		return nil, err
	}
	return &SafeEncoder{enc: enc}, nil
}

// EncodeString encodes a UTF-8 string into shards.
// The string can be ANY valid UTF-8 — emoji, CJK, Arabic, anything.
func (s *SafeEncoder) EncodeString(text string) ([][]byte, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("input is not valid UTF-8")
	}
	return s.EncodeBytes([]byte(text))
}

// EncodeBytes encodes arbitrary bytes into shards (pads as needed).
func (s *SafeEncoder) EncodeBytes(data []byte) ([][]byte, error) {
	padded := pad(data, s.enc.DataShards())
	return s.enc.Encode(padded)
}

// ReconstructString reconstructs the original UTF-8 string from shards.
func (s *SafeEncoder) ReconstructString(shards [][]byte) (string, error) {
	b, err := s.ReconstructBytes(shards)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(string(b)) {
		return "", fmt.Errorf("reconstructed bytes are not valid UTF-8 — data may be corrupted")
	}
	return string(b), nil
}

// ReconstructBytes reconstructs the original bytes from shards.
func (s *SafeEncoder) ReconstructBytes(shards [][]byte) ([]byte, error) {
	padded, err := s.enc.Reconstruct(shards)
	if err != nil {
		return nil, err
	}
	return unpad(padded)
}

// ── Padding helpers ───────────────────────────────────────────────────────────
//
// Layout of padded data:
//
//   [ 4 bytes: original length (big-endian uint32) ][ original data ][ zero padding ]
//
// Example: "Hi🔥" = 6 bytes, dataShards=4
//   header = [0x00, 0x00, 0x00, 0x06]
//   body   = [H, i, 0xF0, 0x9F, 0x94, 0xA5]
//   padded = header + body + [0x00, 0x00]  → 12 bytes (divisible by 4) ✓

const headerSize = 4 // bytes for uint32 original length

func pad(data []byte, k int) []byte {
	origLen := len(data)
	payload := make([]byte, headerSize+origLen)

	// Write original length as 4-byte big-endian header
	binary.BigEndian.PutUint32(payload[:headerSize], uint32(origLen))
	copy(payload[headerSize:], data)

	// Pad to next multiple of k
	rem := len(payload) % k
	if rem != 0 {
		padding := make([]byte, k-rem)
		payload = append(payload, padding...)
	}
	return payload
}

func unpad(padded []byte) ([]byte, error) {
	if len(padded) < headerSize {
		return nil, fmt.Errorf("padded data too short to contain header")
	}
	origLen := int(binary.BigEndian.Uint32(padded[:headerSize]))
	end := headerSize + origLen
	if end > len(padded) {
		return nil, fmt.Errorf("header claims length %d but only %d bytes available",
			origLen, len(padded)-headerSize)
	}
	return padded[headerSize:end], nil
}
