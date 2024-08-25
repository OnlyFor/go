// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rand

import (
	"bytes"
	"compress/flate"
	"crypto/internal/boring"
	"internal/race"
	"io"
	"os"
	"runtime"
	"sync"
	"testing"
)

func testReadAndReader(t *testing.T, f func(*testing.T, func([]byte) (int, error))) {
	t.Run("Read", func(t *testing.T) {
		f(t, Read)
	})
	t.Run("Reader.Read", func(t *testing.T) {
		f(t, Reader.Read)
	})
}

func TestRead(t *testing.T) {
	testReadAndReader(t, testRead)
}

func testRead(t *testing.T, Read func([]byte) (int, error)) {
	var n int = 4e6
	if testing.Short() {
		n = 1e5
	}
	b := make([]byte, n)
	n, err := Read(b)
	if n != len(b) || err != nil {
		t.Fatalf("Read(buf) = %d, %s", n, err)
	}

	var z bytes.Buffer
	f, _ := flate.NewWriter(&z, 5)
	f.Write(b)
	f.Close()
	if z.Len() < len(b)*99/100 {
		t.Fatalf("Compressed %d -> %d", len(b), z.Len())
	}
}

func TestReadLoops(t *testing.T) {
	testReadAndReader(t, testReadLoops)
}

func testReadLoops(t *testing.T, Read func([]byte) (int, error)) {
	b := make([]byte, 1)
	for {
		n, err := Read(b)
		if n != 1 || err != nil {
			t.Fatalf("Read(b) = %d, %v", n, err)
		}
		if b[0] == 42 {
			break
		}
	}
	for {
		n, err := Read(b)
		if n != 1 || err != nil {
			t.Fatalf("Read(b) = %d, %v", n, err)
		}
		if b[0] == 0 {
			break
		}
	}
}

func TestLargeRead(t *testing.T) {
	testReadAndReader(t, testLargeRead)
}

func testLargeRead(t *testing.T, Read func([]byte) (int, error)) {
	// 40MiB, more than the documented maximum of 32Mi-1 on Linux 32-bit.
	b := make([]byte, 40<<20)
	if n, err := Read(b); err != nil {
		t.Fatal(err)
	} else if n != len(b) {
		t.Fatalf("Read(b) = %d, want %d", n, len(b))
	}
}

func TestReadEmpty(t *testing.T) {
	testReadAndReader(t, testReadEmpty)
}

func testReadEmpty(t *testing.T, Read func([]byte) (int, error)) {
	n, err := Read(make([]byte, 0))
	if n != 0 || err != nil {
		t.Fatalf("Read(make([]byte, 0)) = %d, %v", n, err)
	}
	n, err = Read(nil)
	if n != 0 || err != nil {
		t.Fatalf("Read(nil) = %d, %v", n, err)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(b []byte) (int, error) {
	return f(b)
}

func TestReadUsesReader(t *testing.T) {
	var called bool
	defer func(r io.Reader) { Reader = r }(Reader)
	Reader = readerFunc(func(b []byte) (int, error) {
		called = true
		return len(b), nil
	})
	n, err := Read(make([]byte, 32))
	if n != 32 || err != nil {
		t.Fatalf("Read(make([]byte, 32)) = %d, %v", n, err)
	}
	if !called {
		t.Error("Read did not use Reader")
	}
}

func TestConcurrentRead(t *testing.T) {
	testReadAndReader(t, testConcurrentRead)
}

func testConcurrentRead(t *testing.T, Read func([]byte) (int, error)) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	const N = 100
	const M = 1000
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			for i := 0; i < M; i++ {
				b := make([]byte, 32)
				n, err := Read(b)
				if n != 32 || err != nil {
					t.Errorf("Read = %d, %v", n, err)
				}
			}
		}()
	}
	wg.Wait()
}

var sink byte

func TestAllocations(t *testing.T) {
	if boring.Enabled {
		// Might be fixable with https://go.dev/issue/56378.
		t.Skip("boringcrypto allocates")
	}
	if race.Enabled {
		t.Skip("urandomRead allocates under -race")
	}

	n := int(testing.AllocsPerRun(10, func() {
		buf := make([]byte, 32)
		Read(buf)
		sink ^= buf[0]
	}))
	if n > 0 {
		t.Errorf("allocs = %d, want 0", n)
	}
}

// TestNoUrandomFallback ensures the urandom fallback is not reached in
// normal operations.
func TestNoUrandomFallback(t *testing.T) {
	expectFallback := false
	if runtime.GOOS == "aix" {
		// AIX always uses the urandom fallback.
		expectFallback = true
	}
	if os.Getenv("GO_GETRANDOM_DISABLED") == "1" {
		// We are testing the urandom fallback intentionally.
		expectFallback = true
	}
	Read(make([]byte, 1))
	if urandomFile != nil && !expectFallback {
		t.Error("/dev/urandom fallback used unexpectedly")
		t.Log("note: if this test fails, it may be because the system does not have getrandom(2)")
	}
	if urandomFile == nil && expectFallback {
		t.Error("/dev/urandom fallback not used as expected")
	}
}

func BenchmarkRead(b *testing.B) {
	b.Run("4", func(b *testing.B) {
		benchmarkRead(b, 4)
	})
	b.Run("32", func(b *testing.B) {
		benchmarkRead(b, 32)
	})
	b.Run("4K", func(b *testing.B) {
		benchmarkRead(b, 4<<10)
	})
}

func benchmarkRead(b *testing.B, size int) {
	b.SetBytes(int64(size))
	buf := make([]byte, size)
	for i := 0; i < b.N; i++ {
		if _, err := Read(buf); err != nil {
			b.Fatal(err)
		}
	}
}
