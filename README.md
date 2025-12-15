# goconc - Go Concurrency Utilities

[![Go Reference](https://pkg.go.dev/badge/github.com/go-leo/goconc.svg)](https://pkg.go.dev/github.com/go-leo/goconc)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-leo/goconc)](https://goreportcard.com/report/github.com/go-leo/goconc)

goconc is a comprehensive Go concurrency utilities library that provides enhanced synchronization primitives, concurrent data structures, and utility functions to simplify concurrent programming in Go.

## Features

### 1. Barrier
Implements a reusable barrier similar to Java's CyclicBarrier, allowing multiple goroutines to synchronize at a common point.

```go
barrier := barrier.NewGroup(3, func() {
    fmt.Println("All participants have arrived")
})

var wg sync.WaitGroup
for i := 0; i < 3; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        err := barrier.Wait(context.Background())
        if err != nil {
            log.Fatal(err)
        }
    }(i)
}
wg.Wait()
```

### 2. Waiter
Converts blocking Wait operations into channel-based notifications for easier integration with select statements.

```go
// Convert sync.WaitGroup to channel notification
var wg sync.WaitGroup
wg.Add(2)

done := waiter.WaitNotify(&wg)

go func() {
    time.Sleep(100 * time.Millisecond)
    wg.Done()
}()

go func() {
    time.Sleep(200 * time.Millisecond)
    wg.Done()
}()

<-done // Wait for all goroutines to complete
```

### 3. Channel Extensions (chanx)
Provides a rich set of channel operations and utilities for working with channels.

```go
// Merge multiple channels
ch1 := make(chan int)
ch2 := make(chan int)

merged := chanx.FanIn(context.Background(), ch1, ch2)

// Process values from multiple channels
for value := range merged {
    fmt.Println("Received:", value)
}
```

### 4. Async Batch Processing (asyncbatch)
Processes objects either by batch size or time interval asynchronously.

```go
// Create a batch processor
processor, err := asyncbatch.New(
    10,                    // Batch size
    time.Second,           // Interval
    func(p any) {},       // Recovery handler
    func(objs []int) {     // Batch processing function
        fmt.Printf("Processing batch: %v\n", objs)
    },
)
if err != nil {
    log.Fatal(err)
}
defer processor.Close()

// Submit items for batch processing
for i := 0; i < 25; i++ {
    processor.Submit(i)
}
```

### 5. Atomic Operations (atomicx)
Enhanced atomic operations that extend the standard library's atomic package.

```go
var val uint32 = 100

// Decrement atomically
newVal := atomicx.DecrUint32(&val)
fmt.Println("New value:", newVal) // 99
```

### 6. Panic Handling (brave)
Utilities for handling panics in Go programs, allowing recovery from panics and converting them to errors.

```go
// Handle panics with custom recovery
brave.Do(
    func() {
        panic("Something went wrong")
    },
    func(p any, stack []byte) {
        fmt.Printf("Recovered from panic: %v\nStack: %s\n", p, stack)
    },
)
```

### 7. Mutex Extensions (mutexx)
Extended mutex implementations including recursive mutexes, spin locks, and sharded mutexes.

```go
// Recursive mutex
rm := &mutexx.RecursiveMutex{}
rm.Lock()
rm.Lock() // Can lock recursively
rm.Unlock()
rm.Unlock()

// Group mutex for fine-grained locking
gm := &mutexx.GroupMutex{N: 16}
gm.Lock("resource1")
// ... critical section
gm.Unlock("resource1")
```

### 8. Lazy Loading (lazyload)
Implementation of lazy loading with caching and concurrent access control.

```go
// Create a lazy loading group
loader := &lazyload.Group[string]{
    New: func(key string) (string, error) {
        return fmt.Sprintf("Value for %s", key), nil
    },
}

// Load or create value
value, err, loaded := loader.LoadOrNew("key1", nil)
if err != nil {
    log.Fatal(err)
}
fmt.Println(value) // Value for key1
```

### 9. Map Extensions (mapx)
Various concurrent map implementations with different characteristics.

```go
// Thread-safe map with expiration
expMap := mapx.NewExpiredMap(
    mapx.ExpireAfter(func(key any) time.Duration {
        return time.Minute
    }),
)

expMap.Store("key1", "value1")
value, ok := expMap.Load("key1")
```

### 10. Object Pool (poolx)
Generic object pool implementation with custom creation and reset functions.

```go
// Create a pool for custom objects
pool, err := poolx.NewPool(
    func() *bytes.Buffer {
        return &bytes.Buffer{}
    },
    func(buf *bytes.Buffer) {
        buf.Reset()
    },
)
if err != nil {
    log.Fatal(err)
}

// Use the pool
buf := pool.Get()
buf.WriteString("Hello, World!")
pool.Put(buf) // Return to pool after use
```

### 11. Once Execution (oncex)
Ensures functions are executed only once per key with keyed execution.

```go
group := &oncex.Group{}

// Execute function only once per key
group.Do("key1", func() {
    fmt.Println("This will only execute once for key1")
})

// Subsequent calls with the same key won't execute
group.Do("key1", func() {
    fmt.Println("This won't be executed")
})
```

### 12. Goroutine Pool (gofer)
Provides a unified interface for various goroutine pool implementations.

```go
// Create a sample goroutine pool
gofer := sample.New(
    sample.CorePoolSize(5),
    sample.MaximumPoolSize(10),
    sample.KeepAliveTime(60*time.Second)
)

// Submit a task
gofer.Go(func() {
    // Execute task
    fmt.Println("Task executed")
})

// Close the pool gracefully
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
gofer.Close(ctx)
```

## Installation

```bash
go get github.com/go-leo/goconc
```

## Documentation

Full documentation is available at [GoDoc](https://pkg.go.dev/github.com/go-leo/goconc).

## Testing

All packages include comprehensive unit tests ensuring reliability and correctness:

```bash
go test ./...
```

## Benchmarks

Performance benchmarks are included for critical components:

```bash
go test -bench=. ./...
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.