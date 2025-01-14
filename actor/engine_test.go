package actor

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type foo struct{}

func newFoo() Receiver { return &foo{} }

func (foo) Receive(*Context) {}

func TestXxx(t *testing.T) {
	e := NewEngine()
	e.Spawn(newFoo, "foo",
		WithInboxSize(99),
		WithMaxRestarts(1),
		WithTags("1", "2", "bar"),
	)

	time.Sleep(time.Second)
}

func TestProcessInitStartOrder(t *testing.T) {
	var (
		e             = NewEngine()
		wg            = sync.WaitGroup{}
		started, init bool
	)
	pid := e.SpawnFunc(func(c *Context) {
		switch c.Message().(type) {
		case Initialized:
			wg.Add(1)
			init = true
		case Started:
			require.True(t, init)
			started = true
		case int:
			require.True(t, started)
			wg.Done()
		}
	}, "test")
	e.Send(pid, 1)
	wg.Wait()
}

func TestSendWithSender(t *testing.T) {
	e := NewEngine()
	sender := NewPID("local", "foo")
	wg := sync.WaitGroup{}
	wg.Add(1)
	pid := e.Spawn(NewTestProducer(t, func(t *testing.T, ctx *Context) {
		if _, ok := ctx.Message().(string); ok {
			assert.NotNil(t, ctx.Sender())
			assert.Equal(t, sender, ctx.Sender())
			wg.Done()
		}
	}), "test")
	e.SendWithSender(pid, "data", sender)
	wg.Wait()
}

func TestSendMsgRaceCon(t *testing.T) {
	e := NewEngine()
	wg := sync.WaitGroup{}
	pid := e.Spawn(NewTestProducer(t, func(t *testing.T, ctx *Context) {
		msg := ctx.Message()
		if msg == nil {
			fmt.Println("should never happen")
		}
	}), "test")

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			e.Send(pid, []byte("f"))
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestSpawn(t *testing.T) {
	e := NewEngine()
	wg := sync.WaitGroup{}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			tag := strconv.Itoa(i)
			pid := e.Spawn(NewTestProducer(t, func(t *testing.T, ctx *Context) {
			}), "dummy", WithTags(tag))
			e.Send(pid, 1)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func TestPoison(t *testing.T) {
	var (
		e      = NewEngine()
		wg     = sync.WaitGroup{}
		stopwg = sync.WaitGroup{}
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		stopwg.Add(1)
		tag := strconv.Itoa(i)
		pid := e.SpawnFunc(func(c *Context) {
			switch c.Message().(type) {
			case Started:
				wg.Done()
			case Stopped:
				stopwg.Done()
			}
		}, "foo", WithTags(tag))

		wg.Wait()
		e.Poison(pid)
		stopwg.Wait()
		// When a process is poisoned it should be removed from the registry.
		// Hence, we should get the dead letter process here.
		assert.Equal(t, e.deadLetter, e.registry.get(pid))
	}
}

func TestRequestResponse(t *testing.T) {
	e := NewEngine()
	pid := e.Spawn(NewTestProducer(t, func(t *testing.T, ctx *Context) {
		if msg, ok := ctx.Message().(string); ok {
			assert.Equal(t, "foo", msg)
			ctx.Respond("bar")
		}
	}), "dummy")
	resp := e.Request(pid, "foo", time.Millisecond)
	res, err := resp.Result()
	assert.Nil(t, err)
	assert.Equal(t, "bar", res)
	// Response PID should be the dead letter PID. This is because
	// the actual response process that will handle this RPC
	// is deregistered. Test that its actually cleaned up.
	assert.Equal(t, e.deadLetter, e.registry.get(resp.pid))
}

func BenchmarkSendMessageLocal(b *testing.B) {
	e := NewEngine()
	p := NewTestProducer(nil, func(_ *testing.T, _ *Context) {})
	pid := e.Spawn(p, "bench", WithInboxSize(100))

	b.ResetTimer()
	b.Run("x", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			e.Send(pid, pid)
		}
	})
}

func BenchmarkSendWithSenderMessageLocal(b *testing.B) {
	e := NewEngine()
	p := NewTestProducer(nil, func(_ *testing.T, _ *Context) {})
	pid := e.Spawn(p, "bench", WithInboxSize(100))

	for i := 0; i < b.N; i++ {
		e.SendWithSender(pid, pid, pid)
	}
}
