package erasure

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func makeTestFile(t *testing.T, size int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp3")
	data := make([]byte, size)
	rand.Read(data)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileEncodeDecodeNoLoss(t *testing.T) {
	fe, err := NewFileEncoder(4, 2)
	if err != nil {
		t.Fatal(err)
	}
	original := makeTestFile(t, 4096)
	origData, _ := os.ReadFile(original)

	infos, err := fe.EncodeFile(original)
	if err != nil {
		t.Fatalf("EncodeFile: %v", err)
	}
	t.Logf("Encoded into %d shards, each ~%d bytes (+ 64 byte header)", len(infos), infos[0].Size)

	allPaths := make([]string, len(infos))
	for i, info := range infos {
		allPaths[i] = info.Path
	}

	outPath := original + ".recovered"
	if err := fe.ReconstructFile(allPaths, outPath); err != nil {
		t.Fatalf("ReconstructFile: %v", err)
	}
	recovered, _ := os.ReadFile(outPath)
	if string(recovered) != string(origData) {
		t.Error("recovered file does not match original")
	}
	t.Log("✓ No-loss reconstruction passed")
}

func TestFileReconstructWithShardLoss(t *testing.T) {
	fe, _ := NewFileEncoder(4, 2)
	sizes := []int{100, 4095, 44100, 131072}

	for _, size := range sizes {
		size := size
		t.Run(fmt.Sprintf("%d_bytes", size), func(t *testing.T) {
			original := makeTestFile(t, size)
			origData, _ := os.ReadFile(original)

			infos, err := fe.EncodeFile(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}

			// Lose 2 shards — use only 4 of 6
			survivingPaths := []string{
				infos[1].Path,
				infos[2].Path,
				infos[3].Path,
				infos[5].Path,
			}

			outPath := original + ".recovered"
			if err := fe.ReconstructFile(survivingPaths, outPath); err != nil {
				t.Fatalf("reconstruct failed: %v", err)
			}
			recovered, _ := os.ReadFile(outPath)
			if string(recovered) != string(origData) {
				t.Error("recovered file differs from original")
			}
			t.Logf("✓ %d bytes OK", size)
		})
	}
}

func TestFileChecksumDetectsCorruption(t *testing.T) {
	fe, _ := NewFileEncoder(4, 2)
	original := makeTestFile(t, 1024)
	infos, _ := fe.EncodeFile(original)

	// Corrupt payload of shard 1 (header is 64 bytes)
	shardData, _ := os.ReadFile(infos[1].Path)
	shardData[64] ^= 0xFF
	shardData[65] ^= 0xFF
	os.WriteFile(infos[1].Path, shardData, 0644)

	allPaths := make([]string, len(infos))
	for i, info := range infos {
		allPaths[i] = info.Path
	}

	err := fe.ReconstructFile(allPaths, original+".out")
	if err == nil {
		t.Error("expected checksum error, got nil")
	} else {
		t.Logf("✓ Corruption detected: %v", err)
	}
}

func TestFileHeaderMagicRejected(t *testing.T) {
	fe, _ := NewFileEncoder(4, 2)
	original := makeTestFile(t, 512)
	infos, _ := fe.EncodeFile(original)

	// Corrupt the magic "ERSC" in shard 0
	shardData, _ := os.ReadFile(infos[0].Path)
	shardData[0] = 'X'
	os.WriteFile(infos[0].Path, shardData, 0644)

	err := fe.ReconstructFile([]string{infos[0].Path}, original+".out")
	if err == nil {
		t.Error("expected error for bad magic, got nil")
	} else {
		t.Logf("✓ Bad magic rejected: %v", err)
	}
}

func TestFileEncodeDecodeWithRealPNG(t *testing.T) {
	const testFile = "test.png"

	// Read original
	origData, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("test.png not found, skipping: %v", err)
	}
	t.Logf("Original size : %d bytes (%.2f KB)", len(origData), float64(len(origData))/1024)

	// Compute SHA-256 of original BEFORE encoding — our ground truth
	origHash := sha256.Sum256(origData)
	t.Logf("Original SHA-256: %x", origHash)

	fe, _ := NewFileEncoder(4, 2)

	infos, err := fe.EncodeFile(testFile)
	if err != nil {
		t.Fatalf("EncodeFile: %v", err)
	}
	t.Logf("Created %d shards, each %d bytes", len(infos), infos[0].Size)

	// helper — reconstructs, then independently re-checks SHA-256
	checkRecovery := func(name string, shardPaths []string) {
		t.Helper()
		outPath := testFile + "." + name + ".png"
		defer os.Remove(outPath)

		// ReconstructFile internally checks SHA-256 and returns error if mismatch
		if err := fe.ReconstructFile(shardPaths, outPath); err != nil {
			t.Errorf("❌ [%s] ReconstructFile failed (SHA-256 mismatch or I/O): %v", name, err)
			return
		}

		// Read recovered file and verify SHA-256 independently ourselves
		recoveredData, err := os.ReadFile(outPath)
		if err != nil {
			t.Errorf("❌ [%s] could not read recovered file: %v", name, err)
			return
		}
		recoveredHash := sha256.Sum256(recoveredData)
		t.Logf("[%s] Recovered SHA-256: %x", name, recoveredHash)

		if recoveredHash != origHash {
			t.Errorf("❌ [%s] SHA-256 MISMATCH!\n  got:  %x\n  want: %x",
				name, recoveredHash, origHash)
		} else {
			t.Logf("✓  [%s] SHA-256 verified — byte-perfect match", name)
		}

		// Also check raw bytes match
		if !bytes.Equal(recoveredData, origData) {
			t.Errorf("❌ [%s] bytes.Equal failed even though SHA matched (should never happen)", name)
		}
	}

	allPaths := make([]string, len(infos))
	for i, info := range infos {
		allPaths[i] = info.Path
	}
	defer func() {
		for _, info := range infos {
			os.Remove(info.Path)
		}
	}()

	// ── Test 1: all 6 shards present ─────────────────────────────
	checkRecovery("all_shards", allPaths)

	// ── Test 2: lose shard 0 (one data shard gone) ───────────────
	checkRecovery("lose_shard0", allPaths[1:])

	// ── Test 3: lose shards 1 and 5 (one data + one parity) ──────
	checkRecovery("lose_shard1_5", []string{
		allPaths[0], allPaths[2], allPaths[3], allPaths[4],
	})

	// ── Test 4: lose both parity shards ──────────────────────────
	checkRecovery("lose_both_parity", allPaths[:4])

	// Open the recovered image in Preview to visually inspect it
	outPath := testFile + ".final_recovered.png"

	if err := fe.ReconstructFile(allPaths, outPath); err != nil {
		t.Fatalf("final recovery failed: %v", err)
	}

	// Open in Preview (macOS)
	if err := exec.Command("open", outPath).Run(); err != nil {
		t.Logf("could not open image: %v", err)
	}

	t.Logf("Recovered image written to: %s", outPath)

	// ── Test 5: corrupt a shard — SHA must catch it ───────────────
	t.Run("corruption_detected", func(t *testing.T) {
		// Read shard 2, flip some bytes in the payload area
		shardData, _ := os.ReadFile(infos[2].Path)
		corruptedPath := testFile + ".corrupted.shard"
		corrupted := make([]byte, len(shardData))
		copy(corrupted, shardData)
		corrupted[64] ^= 0xFF // payload starts at byte 64 (after header)
		corrupted[65] ^= 0xFF
		os.WriteFile(corruptedPath, corrupted, 0644)
		defer os.Remove(corruptedPath)

		badPaths := []string{
			allPaths[0], allPaths[1],
			corruptedPath, // shard 2 corrupted
			allPaths[3], allPaths[4], allPaths[5],
		}
		outPath := testFile + ".should_not_exist.png"
		defer os.Remove(outPath)

		err := fe.ReconstructFile(badPaths, outPath)
		if err == nil {
			t.Error("❌ expected SHA-256 error for corrupted shard, got nil")
		} else {
			t.Logf("✓  corruption correctly caught: %v", err)
		}
	})
}
