package socket_test

import (
	"fmt"
	"log"
	"time"

	"github.com/zishang520/socket.io-client-go/socket"
)

// ExampleSocket_basic demonstrates the basic usage of Socket.IO client
func ExampleSocket_basic() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	socket.On("connect", func(...any) {
		fmt.Println("Connected!")
	})

	socket.Emit("message", "Hello server!")

	socket.On("reply", func(args ...any) {
		if len(args) > 0 {
			if msg, ok := args[0].(string); ok {
				fmt.Printf("Received: %s\n", msg)
			}
		}
	})

	// Output:
	// Connected!
}

// ExampleSocket_disconnect demonstrates how to disconnect the socket
func ExampleSocket_disconnect() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	socket.On("disconnect", func(args ...any) {
		if len(args) > 0 {
			if reason, ok := args[0].(string); ok {
				fmt.Printf("Disconnected: %s\n", reason)
			}
		}
	})

	socket.Disconnect()

	// Output:
	// Disconnected: io client disconnect
}

// ExampleSocket_emitWithAck demonstrates how to emit events with acknowledgement
func ExampleSocket_emitWithAck() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	socket.EmitWithAck("custom-event", "hello")(func(args []any, err error) {
		if err != nil {
			fmt.Println("Failed to receive ack")
			return
		}
		fmt.Printf("Server acknowledged with: %v\n", args)
	})

	// Output:
	// Server acknowledged with: [received hello]
}

// ExampleSocket_volatile demonstrates how to send messages that may be lost
func ExampleSocket_volatile() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	// The server may or may not receive this message
	socket.Volatile().Emit("hello", "world")
}

// ExampleSocket_onAny demonstrates how to listen to all events
func ExampleSocket_onAny() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	socket.OnAny(func(args ...any) {
		fmt.Printf("Caught event: %v\n", args[0])
	})

	socket.Emit("test-event", "data")

	// Output:
	// Caught event: test-event
}

// ExampleSocket_timeout demonstrates how to set timeout for acknowledgements
func ExampleSocket_timeout() {
	socket, err := socket.Connect("http://localhost:8000", nil)
	if err != nil {
		log.Fatal(err)
	}

	socket.Timeout(5*time.Second).EmitWithAck("delayed-event", "data")(func(args []any, err error) {
		if err != nil {
			fmt.Println("Event timed out")
			return
		}
		fmt.Printf("Received response: %v\n", args)
	})

	// Output:
	// Event timed out
}
