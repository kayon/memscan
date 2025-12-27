package memscan

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestParseRegion(t *testing.T) {
	const rawLine = `00010000-00012000 r--s 00000000 103:08 9639724                           /home/deck/.local/share/Steam/compatibilitytools.d/GE-Proton10-24/files/share/wine/nls/Name   Space 1 `
	fields := strings.Fields(rawLine)

	for _, v := range fields {
		fmt.Printf("%q\n", v)
	}

	m := ParseRegion([]byte(rawLine))

	s, _ := json.MarshalIndent(m, "", " ")
	fmt.Println(string(s))
}

func TestParseV2(t *testing.T) {
	f, err := os.Open("./data/maps")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	m := &Maps{
		exe:  `/home/deck/.local/share/Steam/compatibilitytools.d/GE-Proton10-24/files/bin/wine64-preloader`,
		file: f,
	}
	// 387A0000-38800000 387EDO28
	regions := m.Parse(REGION_ALL_RW)
	var size uint64
	for _, region := range regions {
		size += region.Size
		fmt.Println(region.String())
	}

	fmt.Println("Total size:", size)
}

func TestOptimize(t *testing.T) {
	f, err := os.Open("./data/maps")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	m := &Maps{
		exe:  `/home/deck/.local/share/Steam/compatibilitytools.d/GE-Proton10-24/files/bin/wine64-preloader`,
		file: f,
	}
	// 0AB84A68 98760
	regions := m.Parse(REGION_ALL_RW)
	optimized := RegionsOptimize(regions)

	checkOptimizedRegionsStrict(t, regions, optimized)
}

func checkOptimizedRegionsStrict(t *testing.T, regions []Region, optimized []Region) {
	// 1. Validate Total Byte Conservation
	var totalOri, totalOpt uint64
	for _, r := range regions {
		totalOri += r.Size
	}
	for _, r := range optimized {
		totalOpt += r.Size
	}

	if totalOri != totalOpt {
		t.Fatalf("CRITICAL: Byte count mismatch! Original: %d, Optimized: %d", totalOri, totalOpt)
	}

	// 2. Validate No Internal Overlaps in Optimized Result
	for i := 0; i < len(optimized)-1; i++ {
		if optimized[i+1].Start < optimized[i].End {
			t.Fatalf("LOGIC ERROR: Overlap detected at Block %d [..%X] and Block %d [%X..]",
				i, optimized[i].End, i+1, optimized[i+1].Start)
		}
	}

	// 3. Strict Pointer-Walking Coverage Test
	// This ensures every byte of the original exists in the optimized list in order.
	oIdx := 0
	for _, ori := range regions {
		currentPtr := ori.Start
		remainingSize := ori.Size

		for remainingSize > 0 {
			if oIdx >= len(optimized) {
				t.Fatalf("DATA LOSS: Original range starting at 0x%X is not covered (Optimized list exhausted)", currentPtr)
			}

			opt := optimized[oIdx]

			// Scenario A: Optimized block is behind the current pointer
			if opt.End <= currentPtr {
				oIdx++
				continue
			}

			// Scenario B: Gap detected
			if opt.Start > currentPtr {
				t.Fatalf("GAP DETECTED: Memory hole at 0x%X, next optimized block starts at 0x%X", currentPtr, opt.Start)
			}

			// Scenario C: Overlapping/Covering
			consume := opt.End - currentPtr
			if consume > remainingSize {
				consume = remainingSize
			}

			remainingSize -= consume
			currentPtr += consume

			// If we reached the end of the current optimized chunk, move to the next
			if currentPtr == opt.End {
				oIdx++
			}
		}
	}
	t.Logf("SUCCESS: Strict linear scan passed. %d original -> %d optimized.", len(regions), len(optimized))
}
