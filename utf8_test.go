// utf8_test.go — Tests proving UTF-8 strings survive erasure coding intact.

package erasure

import (
	"strings"
	"testing"
)

// Test strings covering the full UTF-8 spectrum
var utf8TestCases = []struct {
	name string
	text string
}{
	{
		"ascii only",
		"Hello, World!",
	},
	{
		"latin accents (2-byte chars)",
		"Héllo Wörld — café résumé naïve",
	},
	{
		"emoji (4-byte chars)",
		"Fire 🔥 Water 💧 Earth 🌍 Wind 🌪️",
	},
	{
		"chinese characters (3-byte chars)",
		"你好世界 — Hello World in Chinese",
	},
	{
		"arabic (2-byte chars)",
		"مرحبا بالعالم — Hello World in Arabic",
	},
	{
		"mixed multilingual",
		"English • Español • Français • 日本語 • Русский • 한국어 • العربية • 🌐",
	},
	{
		"single emoji",
		"🔥",
	},
	{
		"single ascii char",
		"A",
	},
	{
		"empty string",
		"",
	},
	{
		"newlines and tabs",
		"line1\nline2\ttabbed\r\nwindows newline",
	},
	{
		"long text",
		strings.Repeat("The quick brown fox jumps over the lazy dog. 🦊 ", 20),
	},
}

func TestUTF8EncodeDecodeNoLoss(t *testing.T) {
	enc, err := NewSafeEncoder(4, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range utf8TestCases {
		t.Run(tc.name, func(t *testing.T) {
			shards, err := enc.EncodeString(tc.text)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}
			recovered, err := enc.ReconstructString(shards)
			if err != nil {
				t.Fatalf("reconstruct failed: %v", err)
			}
			if recovered != tc.text {
				t.Errorf("mismatch:\n got:  %q\nwant: %q", recovered, tc.text)
			}
		})
	}
}

func TestUTF8WithShardLoss(t *testing.T) {
	enc, err := NewSafeEncoder(4, 2)
	if err != nil {
		t.Fatal(err)
	}

	text := "Hello 🔥 世界 — multilingual test with emoji! café résumé"
	t.Logf("Original : %q", text)
	t.Logf("Byte length: %d", len([]byte(text)))

	shards, err := enc.EncodeString(text)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Try every combination of losing 2 shards
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			// Deep copy shards
			s := make([][]byte, 6)
			for k, sh := range shards {
				if k == i || k == j {
					s[k] = nil // lost
				} else {
					s[k] = make([]byte, len(sh))
					copy(s[k], sh)
				}
			}
			recovered, err := enc.ReconstructString(s)
			if err != nil {
				t.Errorf("lost {%d,%d}: reconstruct failed: %v", i, j, err)
				continue
			}
			if recovered != text {
				t.Errorf("lost {%d,%d}: text mismatch", i, j)
			}
		}
	}
	t.Log("✓ All 15 combinations of 2-shard loss recovered correctly")
}

func TestUTF8PaddingLayout(t *testing.T) {
	// Verify the header correctly stores original length
	cases := []string{
		"A",          // 1 byte
		"é",          // 2 bytes
		"中",          // 3 bytes
		"🔥",         // 4 bytes
		"Hello 🔥",   // 9 bytes
	}
	for _, s := range cases {
		orig := []byte(s)
		padded := pad(orig, 4)
		unpadded, err := unpad(padded)
		if err != nil {
			t.Errorf("%q: unpad error: %v", s, err)
			continue
		}
		if string(unpadded) != s {
			t.Errorf("%q: roundtrip failed, got %q", s, unpadded)
		}
		t.Logf("%q: orig=%d bytes, padded=%d bytes ✓", s, len(orig), len(padded))
	}
}

func TestUTF8EmptyString(t *testing.T) {
	enc, _ := NewSafeEncoder(4, 2)
	shards, err := enc.EncodeString("")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := enc.ReconstructString(shards)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != "" {
		t.Errorf("expected empty string, got %q", recovered)
	}
}

func TestUTF8InvalidBytes(t *testing.T) {
	enc, _ := NewSafeEncoder(4, 2)
	// 0xFF is not valid UTF-8
	invalidUTF8 := string([]byte{0xFF, 0xFE, 0x41})
	_, err := enc.EncodeString(invalidUTF8)
	if err == nil {
		t.Error("expected error for invalid UTF-8 input, got nil")
	} else {
		t.Logf("correctly rejected invalid UTF-8: %v", err)
	}
}

func BenchmarkUTF8Encode(b *testing.B) {
	enc, _ := NewSafeEncoder(4, 2)
	text := strings.Repeat("Hello 🔥 世界 café — ", 100)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.EncodeString(text) //nolint
	}
}
