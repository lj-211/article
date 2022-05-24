# Golang - How to close channel gracefully

## 前文回顾
[Channel实现分析](Part.01.E.Golang-channel.md) 中提到的我们在使用channel可能会碰到的问题：

>nil的channel 如果是阻塞读写可能导致协程永远挂起
>写已关闭的channel会直接panic
>读已关闭的channel，如果有数据则正常返回，如果没有数据则返回0
>关闭同一个channel两次会导致panic

### 可能要避免的情形
#### 操作nil channel
1. block读写都会导致协程永远被挂起

#### 操作已关闭的channel
1. Sending to closed channel will cause panic
2. 读一个已关闭的chan是没有问题的

#### 操作已经close的channel
1. 关闭nil chan会panic
2. 关闭已关闭chan会panic

#### 关闭channel
1. 唤醒读协程
2. 唤醒写协程

以上两个后果会导致操作已关闭的channel

## 如何规避这类问题

通过上面的总结，我们可以看到，对于channel而言，对于nil channel的操作以及关闭已关闭的channel是比较容易避免的。

读非nil的channel是安全的，写协程操作是比较危险的；复杂的情形是，读写协程同时存在时，我们要避免关闭协程导致写协程panic。

下面是比较常见的情形：

### 1 write 1 recv 
``` go
```
### n write 1 recv

**Example 1: 等待所有的写完成后关闭chan**

``` go
func tchan_4_onewrite_onerecv() {
	sender := func(n int, c1, c2, c3, c4 chan<- int) {
		defer close(c1)
		defer close(c2)
		defer close(c3)
		defer close(c4)

		for i := 0; i < n; i++ {
			select {
			case c1 <- i:
			case c2 <- i:
			case c3 <- i:
			case c4 <- i:
			}
		}
	}

	consumer := func(w chan<- int, input <-chan int, done chan<- bool) {
		for ival := range input {
			w <- ival
		}

		done <- true
	}

	c1 := make(chan int)
	c2 := make(chan int)
	c3 := make(chan int)
	c4 := make(chan int)

	go sender(100, c1, c2, c3, c4)

	wchan := make(chan int)
	done := make(chan bool)
	go consumer(wchan, c1, done)
	go consumer(wchan, c2, done)
	go consumer(wchan, c3, done)
	go consumer(wchan, c4, done)

	go func() {
		<-done
		<-done
		<-done
		<-done
		close(wchan)
	}()

	for wval := range wchan {
		fmt.Println(wval)
	}
}
```

**Example 2: 由外部控制写(使用一个stop channel)，并决定停止写，之后关闭chan**
``` go
func tchan_4_nwrite_onerecv_1() {
	tchan := make(chan int)
	stop := make(chan bool)
	var wg sync.WaitGroup

	writer := func(val int, wchan chan<- int, stop <-chan bool) {
        defer wg.Done()
	LOOP:
		for {
			select {
			case <-stop:
				fmt.Println(val, " receive stop cmd")
				break LOOP
			default:
			}

			select {
			case wchan <- val:
				fmt.Println("exec: ", val)
			case <-stop:
				fmt.Println(val, " receive stop cmd")
				break LOOP
			default:
			}
		}
	}

    wg.Add(4)
	go writer(1, tchan, stop)
	go writer(2, tchan, stop)
	go writer(3, tchan, stop)
	go writer(4, tchan, stop)

	receiver := func(wchan <-chan int, stop chan<- bool) {
		count := 0
		for val := range wchan {
			fmt.Println("receive: ", val)
			count++

			if count == 123 {
				break
			}
		}

		close(stop)
	}
	go receiver(tchan, stop)

    wg.Wait()
	close(tchan)
}
```

**Example 3: 由外部控制写(使用cancel context)，并决定停止写，之后关闭chan**
``` go
func tchan_4_nwrite_onerecv() {
	var wg sync.WaitGroup
	wfunc := func(ctx context.Context, wchan chan<- int, val int) {
		defer wg.Done()
		for {
			select {
			case wchan <- val:
			case <-ctx.Done():
				fmt.Println("Cancel: ", val)
				return
			}
		}
	}
	wchan := make(chan int)
	parent, pcancel := context.WithCancel(context.Background())
	wg.Add(1)
	go wfunc(parent, wchan, 100)
	c, _ := context.WithCancel(parent)
	wg.Add(1)
	go wfunc(c, wchan, 321)

	go func() {
		for ival := range wchan {
			fmt.Println("ival: ", ival)
		}
	}()

	pcancel()

	wg.Wait()

	close(wchan)
}
```

### 1 write n recv
``` go
func tchan_4_onewrite_nrecv() {
	sender := func(n int, c1 chan<- int, notify func()) {
		for i := 0; i < n; i++ {
			select {
			case c1 <- i:
				// do nothing
			}
		}
		notify()
	}

	tchan := make(chan int)
	go sender(11, tchan, func() {
		close(tchan)
	})

	var wg sync.WaitGroup
	receiver := func(tchan <-chan int) {
		defer wg.Done()
		for ival := range tchan {
			fmt.Println("received: ", ival)
		}
	}

	wg.Add(4)
	go receiver(tchan)
	go receiver(tchan)
	go receiver(tchan)
	go receiver(tchan)

	wg.Wait()
}
```

## 总结
1. 关闭对写的影响大于读，首先要保证writer能合理退出
