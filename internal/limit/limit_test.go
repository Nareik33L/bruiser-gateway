package limit

import (
	"testing"
	"time"
)

func TestTakeInFlight(t *testing.T) {
	p := New(2, 100)
	now := time.Now()
	r1, ok := p.Take("exe", now)
	if !ok {
		t.Fatal("first")
	}
	r2, ok := p.Take("exe", now)
	if !ok {
		t.Fatal("second")
	}
	if _, ok := p.Take("exe", now); ok {
		t.Fatal("third should 429")
	}
	r1()
	r2()
	if _, ok := p.Take("exe", now); !ok {
		t.Fatal("after release")
	}
}

func TestTakeRate(t *testing.T) {
	p := New(10, 1)
	now := time.Now()
	if _, ok := p.Take("exe", now); !ok {
		t.Fatal("first token")
	}
	if _, ok := p.Take("exe", now); ok {
		t.Fatal("same instant should 429 at 1 rps")
	}
	if _, ok := p.Take("exe", now.Add(time.Second)); !ok {
		t.Fatal("after 1s")
	}
}

func TestConcurrentDistinct(t *testing.T) {
	p := New(1, 5)
	now := time.Now()
	for i := 0; i < 20; i++ {
		id := "exe-" + string(rune('a'+i))
		if _, ok := p.Take(id, now); !ok {
			t.Fatalf("distinct %s", id)
		}
	}
}
