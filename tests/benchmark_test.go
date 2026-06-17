package tests

import (
	"fmt"
	"testing"

	"github.com/you/madonna/internal/store"
)

// BenchmarkStorePut measures raw put throughput (WAL + memory).
func BenchmarkStorePut(b *testing.B) {
	st, err := store.New(b.TempDir() + "/wal.log")
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i)
			_ = st.Put(key, "some-value")
			i++
		}
	})
}

// BenchmarkStoreGet measures read throughput from the in-memory map.
func BenchmarkStoreGet(b *testing.B) {
	st, err := store.New(b.TempDir() + "/wal.log")
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		_ = st.Put(fmt.Sprintf("key-%d", i), "value")
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			st.Get(fmt.Sprintf("key-%d", i%10000))
			i++
		}
	})
}
