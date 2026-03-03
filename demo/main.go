package main

import (
	"fmt"
	"strings"

	erasure "erasure_code"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║         Reed-Solomon Erasure Coding Demo (GF(2^8))           ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// ── Demo 1: Basic 4+2 configuration ──────────────────────────────────────
	fmt.Println("\n▶  Demo 1: 4 data shards + 2 parity shards (can lose any 2)")
	fmt.Println(strings.Repeat("─", 60))

	enc, _ := erasure.NewEncoder(4, 2)
	original := []byte("Hello, Erasure Coding! [32 byte]") // exactly 32 bytes
	fmt.Printf("Original data  : %q\n", original)
	fmt.Printf("Data shards    : 4   (K)\n")
	fmt.Printf("Parity shards  : 2   (M)\n")
	fmt.Printf("Total shards   : 6   (N = K+M)\n")
	fmt.Printf("Fault tolerance: any 2 shards can be lost\n\n")

	shards, err := enc.Encode(original)
	if err != nil {
		fmt.Printf("Encode error: %v\n", err)
		return
	}
	for i, s := range shards {
		label := "data  "
		if i >= 4 {
			label = "parity"
		}
		fmt.Printf("  Shard[%d] (%s): %v\n", i, label, s)
	}

	// Simulate losing shards 0 and 5 (one data + one parity)
	fmt.Println("\n  ✗ Losing shard[0] (data) and shard[5] (parity)...")
	shards[0] = nil
	shards[5] = nil

	recovered, err := enc.Reconstruct(shards)
	if err != nil {
		fmt.Printf("  ✗ FAILED: %v\n", err)
	} else {
		fmt.Printf("  ✓ Recovered: %q\n", recovered)
		match := string(recovered) == string(original)
		fmt.Printf("  ✓ Match: %v\n", match)
	}

	// ── Demo 2: Losing ALL data shards (only parity remains) ─────────────────
	fmt.Println("\n▶  Demo 2: Recovering from ONLY parity shards")
	fmt.Println(strings.Repeat("─", 60))

	enc2, _ := erasure.NewEncoder(2, 4)
	msg := []byte("DEADBEEF") // 8 bytes → 2 shards of 4 bytes each
	fmt.Printf("Data: %q  |  Config: 2 data + 4 parity\n", msg)

	shards2, _ := enc2.Encode(msg)
	fmt.Println("Encoded shards:")
	for i, s := range shards2 {
		fmt.Printf("  [%d]: %v\n", i, s)
	}

	shards2[0] = nil
	shards2[1] = nil
	fmt.Println("  ✗ Lost BOTH data shards — relying entirely on parity...")

	rec2, err := enc2.Reconstruct(shards2)
	if err != nil {
		fmt.Printf("  ✗ FAILED: %v\n", err)
	} else {
		fmt.Printf("  ✓ Recovered from parity only: %q\n", rec2)
	}

	// ── Demo 3: Beyond the fault tolerance limit ─────────────────────────────
	fmt.Println("\n▶  Demo 3: What happens when too many shards are lost?")
	fmt.Println(strings.Repeat("─", 60))

	enc3, _ := erasure.NewEncoder(4, 2)
	shards3, _ := enc3.Encode(original)
	shards3[0] = nil
	shards3[1] = nil
	shards3[2] = nil
	fmt.Println("  ✗ Lost 3 shards (max allowed is 2)...")

	_, err = enc3.Reconstruct(shards3)
	if err != nil {
		fmt.Printf("  ✓ Correctly rejected: %v\n", err)
	}

	// ── How it works ──────────────────────────────────────────────────────────
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  How Reed-Solomon works (the math in 4 lines)                ║")
	fmt.Println("║                                                               ║")
	fmt.Println("║  1. GF(2^8): add=XOR, multiply via log/anti-log tables       ║")
	fmt.Println("║  2. Cauchy matrix: C[i][j] = 1/(x[i] XOR y[j])              ║")
	fmt.Println("║  3. Any K×K sub-matrix of a Cauchy matrix is invertible      ║")
	fmt.Println("║  4. Decode = invert K×K sub-matrix, multiply by K shards     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}
