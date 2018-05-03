package demoinfocs_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dem "github.com/markus-wa/demoinfocs-golang"
	events "github.com/markus-wa/demoinfocs-golang/events"
)

const csDemosPath = "test/cs-demos"
const demSetPath = csDemosPath + "/set"
const defaultDemPath = csDemosPath + "/default.dem"
const unexpectedEndOfDemoPath = csDemosPath + "/unexpected_end_of_demo.dem"

func init() {
	if _, err := os.Stat(defaultDemPath); err != nil {
		panic(fmt.Sprintf("Failed to read test demo %q", defaultDemPath))
	}
}

func TestDemoInfoCs(t *testing.T) {
	f, err := os.Open(defaultDemPath)
	defer f.Close()
	if err != nil {
		t.Fatal(err)
	}

	p := dem.NewParser(f, dem.WarnToStdErr)

	fmt.Println("Parsing header")
	p.RegisterEventHandler(func(e events.HeaderParsedEvent) {
		fmt.Printf("Header: %v\n", e)
	})
	p.ParseHeader()

	fmt.Println("Registering handlers")
	var tState *dem.TeamState
	var ctState *dem.TeamState
	var oldTScore int
	var oldCtScore int
	p.RegisterEventHandler(func(events.TickDoneEvent) {
		if tState != nil && oldTScore != tState.Score() {
			fmt.Println("T-side score:", tState.Score())
			oldTScore = tState.Score()
		} else if ctState != nil && oldCtScore != ctState.Score() {
			fmt.Println("CT-side score:", ctState.Score())
			oldCtScore = ctState.Score()
		}
	})
	tState = p.TState()
	ctState = p.CTState()

	ts := time.Now()
	var done int64
	go func() {
		// 5 minute timeout (for a really slow machine with race condition testing)
		timer := time.NewTimer(time.Minute * 5)
		<-timer.C
		if atomic.LoadInt64(&done) == 0 {
			t.Error("Parsing timeout")
			p.Cancel()
			timer.Reset(time.Second * 1)
			<-timer.C
			t.Fatal("Parser locked up for more than one second after cancellation")
		}
	}()

	frameByFrameCount := 1000
	fmt.Printf("Parsing frame by frame (%d frames)\n", frameByFrameCount)
	for i := 0; i < frameByFrameCount; i++ {
		ok, err := p.ParseNextFrame()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("Parser reported end of demo after less than %d frames", frameByFrameCount)
		}
	}

	fmt.Println("Parsing to end")
	err = p.ParseToEnd()
	if err != nil {
		t.Fatal(err)
	}

	atomic.StoreInt64(&done, 1)
	fmt.Printf("Took %s\n", time.Since(ts))
}

func TestUnexpectedEndOfDemo(t *testing.T) {
	f, err := os.Open(unexpectedEndOfDemoPath)
	defer f.Close()
	if err != nil {
		t.Fatal(err)
	}

	p := dem.NewParser(f, nil)
	_, err = p.ParseHeader()
	if err != nil {
		t.Fatal(err)
	}

	err = p.ParseToEnd()
	if err != dem.ErrUnexpectedEndOfDemo {
		t.Fatal("Parsing cancelled but error was not ErrUnexpectedEndOfDemo:", err)
	}
}

func TestCancelParseToEnd(t *testing.T) {
	f, err := os.Open(defaultDemPath)
	defer f.Close()
	if err != nil {
		t.Fatal(err)
	}

	p := dem.NewParser(f, nil)
	_, err = p.ParseHeader()
	if err != nil {
		t.Fatal(err)
	}

	maxTicks := 100
	var tix int

	p.RegisterEventHandler(func(events.TickDoneEvent) {
		tix++
		if tix == maxTicks {
			p.Cancel()
		}
	})

	err = p.ParseToEnd()
	if err != dem.ErrCancelled {
		t.Fatal("Parsing cancelled but error was not ErrCancelled:", err)
	}
}

func TestConcurrent(t *testing.T) {
	var i int64
	runner := func() {
		f, err := os.Open(defaultDemPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		p := dem.NewParser(f, nil)

		_, err = p.ParseHeader()
		if err != nil {
			t.Fatal(err)
		}

		n := atomic.AddInt64(&i, 1)
		fmt.Printf("Starting runner %d\n", n)

		ts := time.Now()

		err = p.ParseToEnd()
		if err != nil {
			t.Fatal(err)
		}

		fmt.Printf("Runner %d took %s\n", n, time.Since(ts))
	}

	var wg sync.WaitGroup
	for j := 0; j < 2; j++ {
		wg.Add(1)
		go func() { runner(); wg.Done() }()
	}
	wg.Wait()
}

func TestDemoSet(t *testing.T) {
	dems, err := ioutil.ReadDir(demSetPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range dems {
		name := d.Name()
		if strings.HasSuffix(name, ".dem") {
			fmt.Printf("Parsing '%s/%s'\n", demSetPath, name)
			func() {
				var f *os.File
				f, err = os.Open(demSetPath + "/" + name)
				defer f.Close()
				if err != nil {
					t.Error(err)
				}

				defer func() {
					pErr := recover()
					if pErr != nil {
						t.Errorf("Failed to parse '%s/%s' - %s\n", demSetPath, name, pErr)
					}
				}()

				p := dem.NewParser(f, nil)
				_, err = p.ParseHeader()
				if err != nil {
					t.Fatal(err)
				}

				err = p.ParseToEnd()
				if err != nil {
					t.Fatal(err)
				}
			}()
		}
	}
}

func BenchmarkDemoInfoCs(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			f, err := os.Open(defaultDemPath)
			defer f.Close()
			if err != nil {
				b.Fatal(err)
			}

			p := dem.NewParser(f, nil)

			_, err = p.ParseHeader()
			if err != nil {
				b.Fatal(err)
			}

			ts := time.Now()

			err = p.ParseToEnd()
			if err != nil {
				b.Fatal(err)
			}

			b.Logf("Took %s\n", time.Since(ts))
		}()
	}
}

func BenchmarkInMemory(b *testing.B) {
	f, err := os.Open(defaultDemPath)
	defer f.Close()
	if err != nil {
		b.Fatal(err)
	}

	inf, err := f.Stat()
	if err != nil {
		b.Fatal(err)
	}

	d := make([]byte, inf.Size())
	n, err := f.Read(d)
	if err != nil || int64(n) != inf.Size() {
		b.Fatal(fmt.Sprintf("Expected %d bytes, got %d", inf.Size(), n), err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := dem.NewParser(bytes.NewReader(d), nil)

		_, err = p.ParseHeader()
		if err != nil {
			b.Fatal(err)
		}

		ts := time.Now()

		err = p.ParseToEnd()
		if err != nil {
			b.Fatal(err)
		}

		b.Logf("Took %s\n", time.Since(ts))
	}
}
