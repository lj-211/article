package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("test chan")

	//tchan_1()

	//tchan_2_recv()
	//tchan_2_send()

	//go tchan_3()
	//for {
	//time.Sleep(time.Second * 2)
	//}

	// n writer 1 receiver
	//tchan_4_onewrite_onerecv()
	//tchan_4_nwrite_onerecv_1()
	//tchan_4_nwrite_onerecv_2()
	tchan_4_onewrite_nrecv()
	//tchan_4_nwrite_nrecv()

	//tchan_5()
}

// check deadlock
func tchan_1() {
	tchan := make(chan int, 10)

	for {
		ival, ok := <-tchan

		time.Sleep(time.Second)

		fmt.Println("tick: ", ival, " and ", ok)
	}
}

// unblock recv
func tchan_2_recv() {
	tchan := make(chan int)

	select {
	case ival, ok := <-tchan:
		fmt.Println("select: ", ival, " and ", ok)
	default:
		fmt.Println("default")
	}

	// with timeout
	timeout := time.NewTimer(time.Second * 5)
	select {
	case ival, ok := <-tchan:
		fmt.Println("select: ", ival, " and ", ok)
	case _ = <-timeout.C:
		fmt.Println("time out")
	}
}

// unblock send
func tchan_2_send() {
	tchan := make(chan int)

	// cause: all goroutines are asleep - deadlock!
	// tchan <- 100
	select {
	case tchan <- 100:
		fmt.Println("send ok")
	default:
		fmt.Println("send fail")
	}

	fmt.Println("tchan_2_send")
}

// read close chan
func tchan_3() {
	tchan := make(chan int, 1)
	tchan <- 100
	close(tchan)

	ival, ok := <-tchan
	fmt.Println("read: ", ival, " and ", ok)

	ival, ok = <-tchan
	fmt.Println("read: ", ival, " and ", ok)
}

// 1 write 1 recv
// consumer notify
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

	for {
		time.Sleep(time.Second * 14)
	}

	close(tchan)
}

func tchan_4_nwrite_onerecv_2() {
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

func tchan_5() {
	chanchan := func() {
		cc := make(chan chan int, 1)
		c := make(chan int, 1)
		cc <- c
		select {
		case <-cc <- 2:
			fmt.Println("expression write")
		default:
			panic("nonblock")
		}
		if <-c != 2 {
			panic("bad receive")
		}
	}
	sendprec := func() {
		c := make(chan bool, 1)
		c <- false || true // not a syntax error: same as c <- (false || true)
		if !<-c {
			panic("sent false")
		} else {
			fmt.Println("expression")
		}
	}

	chanchan()
	sendprec()
}
