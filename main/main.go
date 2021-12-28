// A demo to executing lrpc
package main

import (
	"context"
	"fmt"
	"log"
	"lrpc"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	var foo Foo
	// pick a free port
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	if err := lrpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	lrpc.HandleHTTP()
	addr <- l.Addr().String()
	_ = http.Serve(l, nil)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	// rpc client
	client, _ := lrpc.DialHTTP("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			var reply int
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			defer cancel()
			log.Printf("%d + %d = %d:", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()

	var quit string
	fmt.Scanln(&quit)
}
