package worldgen

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// BenchmarkLoadRadius loads every chunk within 32 chunks of the origin,
// nearest first, from 16 goroutines through a TerrainCache, the way a
// server fills a player's view. It reports chunks per second and the heap
// the cache holds afterwards.
func BenchmarkLoadRadius(b *testing.B) {
	reg, ids := testRegistry(b)
	o, err := NewOverworld(12345, reg, ids)
	if err != nil {
		b.Fatal(err)
	}
	const r = 32
	var pos [][2]int32
	for x := int32(-r); x <= r; x++ {
		for z := int32(-r); z <= r; z++ {
			if x*x+z*z <= r*r {
				pos = append(pos, [2]int32{x, z})
			}
		}
	}
	slices.SortStableFunc(pos, func(a, b [2]int32) int {
		return int(a[0]*a[0] + a[1]*a[1] - b[0]*b[0] - b[1]*b[1])
	})
	var heap uint64
	b.ResetTimer()
	for range b.N {
		runtime.GC()
		var m0 runtime.MemStats
		runtime.ReadMemStats(&m0)
		cache := NewTerrainCache(o, 1024)
		var next atomic.Int64
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				var c Chunk
				for {
					i := next.Add(1) - 1
					if int(i) >= len(pos) {
						return
					}
					cache.Generate(pos[i][0], pos[i][1], &c)
				}
			})
		}
		wg.Wait()
		b.StopTimer()
		runtime.GC()
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)
		heap = m1.HeapAlloc - min(m0.HeapAlloc, m1.HeapAlloc)
		runtime.KeepAlive(cache)
		b.StartTimer()
	}
	b.ReportMetric(float64(len(pos)*b.N)/b.Elapsed().Seconds(), "chunks/s")
	b.ReportMetric(float64(heap)/1e6, "heapMB")
}
