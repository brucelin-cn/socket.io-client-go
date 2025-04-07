package socket

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zishang520/engine.io-go-parser/packet"
	"github.com/zishang520/engine.io/v2/events"
	"github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/engine.io/v2/utils"
	"github.com/zishang520/socket.io-go-parser/v2/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

// A Socket is the fundamental class for interacting with the server.
//
// A Socket belongs to a certain Namespace (by default /) and uses an underlying {@link Manager} to communicate.
//
// Example:
//
//	const socket = io()
//
//	socket.on("connect", () => {
//		console.log("connected")
//	})
//
//	// send an event to the server
//	socket.emit("foo", "bar")
//
//	socket.on("foobar", () => {
//		// an event was received from the server
//	})
//
//	// upon disconnection
//	socket.on("disconnect", (reason) => {
//		console.log(`disconnected due to ${reason}`)
//	})
type Socket struct {
	types.EventEmitter

	// Public
	//
	// Readonly
	io *Manager

	// A unique identifier for the session. `undefined` when the socket is not connected.
	//
	// Example:
	// const socket = io();
	//
	// console.log(socket.id); // undefined
	//
	// socket.on("connect", () => {
	//   console.log(socket.id); // "G5p5..."
	// });
	//
	// Public
	id atomic.Value

	// The session ID used for connection state recovery, which must not be shared (unlike [id]).
	//
	// Private
	_pid string

	// The offset of the last received packet, which will be sent upon reconnection to allow for the recovery of the connection state.
	//
	// Private
	_lastOffset string

	// Whether the socket is currently connected to the server.
	//
	// Example:
	// const socket = io();
	//
	// socket.on("connect", () => {
	//   console.log(socket.connected); // true
	// });
	//
	// socket.on("disconnect", () => {
	//   console.log(socket.connected); // false
	// });
	//
	// Public
	connected atomic.Bool

	// Whether the connection state was recovered after a temporary disconnection. In that case, any missed packets will
	// be transmitted by the server.
	//
	// Public
	recovered atomic.Bool

	// Credentials that are sent when accessing a namespace.
	//
	// Example:
	// const socket = io({
	//   auth: {
	//     token: "abcd"
	//   }
	// });
	//
	// // or with a function
	// const socket = io({
	//   auth: (cb) => {
	//     cb({ token: localStorage.token })
	//   }
	// });
	//
	// Public
	auth map[string]any

	// Buffer for packets received before the CONNECT packet
	//
	// Public
	receiveBuffer *types.Slice[[]any]

	// Buffer for packets that will be sent once the socket is connected
	//
	// Public
	sendBuffer *types.Slice[*Packet]

	// The _queue of packets to be sent with retry in case of failure.
	//
	// Packets are sent one by one, each waiting for the server acknowledgement, in order to guarantee the delivery order.
	//
	// Private
	_queue *types.Slice[*QueuedPacket]

	// A sequence to generate the ID of the {@link QueuedPacket}.
	//
	// Private
	_queueSeq atomic.Uint64

	// Private
	//
	// Readonly
	nsp string
	// Private
	//
	// Readonly
	_opts SocketOptionsInterface

	// Private
	ids atomic.Uint64

	// A map containing acknowledgement handlers.
	//
	// The `withError` attribute is used to differentiate handlers that accept an error as first argument:
	//
	// - `socket.emit("test", (err, value) => { ... })` with `ackTimeout` option
	// - `socket.timeout(5000).emit("test", (err, value) => { ... })`
	// - `const value = await socket.emitWithAck("test")`
	//
	// From those that don't:
	//
	// - `socket.emit("test", (value) => { ... });`
	//
	// In the first case, the handlers will be called with an error when:
	//
	// - the timeout is reached
	// - the socket gets disconnected
	//
	// In the second case, the handlers will be simply discarded upon disconnection, since the client will never receive
	// an acknowledgement from the server.
	//
	//
	// Private
	acks *types.Map[uint64, socket.Ack]
	// Private
	flags atomic.Pointer[Flags]
	// Private
	subs atomic.Pointer[types.Slice[types.Callable]]
	// Private
	_anyListeners *types.Slice[events.Listener]
	// Private
	_anyOutgoingListeners *types.Slice[events.Listener]
}

func MakeSocket() *Socket {
	r := &Socket{
		EventEmitter: types.NewEventEmitter(),

		receiveBuffer:         types.NewSlice[[]any](),
		sendBuffer:            types.NewSlice[*Packet](),
		_queue:                types.NewSlice[*QueuedPacket](),
		acks:                  &types.Map[uint64, socket.Ack]{},
		_anyListeners:         types.NewSlice[events.Listener](),
		_anyOutgoingListeners: types.NewSlice[events.Listener](),
	}

	r.flags.Store(&Flags{})
	return r
}

func NewSocket(io *Manager, nsp string, opts SocketOptionsInterface) *Socket {
	r := MakeSocket()

	r.Construct(io, nsp, opts)

	return r
}

func (s *Socket) Io() *Manager {
	return s.io
}

func (s *Socket) Id() socket.SocketId {
	if id := s.id.Load(); id != nil {
		return id.(socket.SocketId)
	}
	return ""
}

func (s *Socket) Connected() bool {
	return s.connected.Load()
}

func (s *Socket) Recovered() bool {
	return s.recovered.Load()
}

func (s *Socket) Auth() map[string]any {
	return s.auth
}

func (s *Socket) ReceiveBuffer() *types.Slice[[]any] {
	return s.receiveBuffer
}

func (s *Socket) SendBuffer() *types.Slice[*Packet] {
	return s.sendBuffer
}

// Socket` constructor.
func (s *Socket) Construct(io *Manager, nsp string, opts SocketOptionsInterface) {
	s.io = io
	s.nsp = nsp
	if opts != nil && opts.GetRawAuth() != nil {
		s.auth = opts.Auth()
	}
	s._opts = DefaultSocketOptions().Assign(opts)
	if s.io._autoConnect {
		s.Open()
	}
}

// Whether the socket is currently disconnected
//
// Example:
// const socket = io()
//
//	socket.on("connect", () => {
//	  console.log(socket.disconnected) // false
//	})
//
//	socket.on("disconnect", () => {
//	  console.log(socket.disconnected) // true
//	})
func (s *Socket) Disconnected() bool {
	return !s.connected.Load()
}

// Subscribe to open, close and packet events
func (s *Socket) subEvents() {
	if s.Active() {
		return
	}

	s.subs.Store(types.NewSlice(
		on(s.io, "open", s.onopen),
		on(s.io, "packet", func(args ...any) {
			if len(args) > 0 {
				s.onpacket(args[0].(*parser.Packet))
			}
		}),
		on(s.io, "error", s.onerror),
		on(s.io, "close", func(args ...any) {
			s.onclose(args[0].(string), args[1].(error))
		}),
	))
}

// Whether the Socket will try to reconnect when its Manager connects or reconnects.
//
// Example:
// const socket = io();
//
// console.log(socket.active); // true
//
//	socket.on("disconnect", (reason) => {
//	  if (reason === "io server disconnect") {
//	    // the disconnection was initiated by the server, you need to manually reconnect
//	    console.log(socket.active); // false
//	  }
//	  // else the socket will automatically try to reconnect
//	  console.log(socket.active); // true
//	});
func (s *Socket) Active() bool {
	return s.subs.Load() != nil
}

// "Opens" the socket.
//
// Example:
//
//	const socket = io({
//	  autoConnect: false
//	});
//
// socket.connect();
func (s *Socket) Connect() *Socket {
	if s.connected.Load() {
		return s
	}

	s.subEvents()
	if !s.io._reconnecting.Load() {
		s.io.Open(nil) // ensure open
	}
	if ReadyStateOpen == s.io._readyState.Load() {
		s.onopen()
	}
	return s
}

// Alias for [Socket.Connect].
func (s *Socket) Open() *Socket {
	return s.Connect()
}

// Sends a `message` event.
//
// This method mimics the WebSocket.send() method.
//
// See: https://developer.mozilla.org/en-US/docs/Web/API/WebSocket/send
//
// Example:
// socket.send("hello");
//
// // this is equivalent to
// socket.emit("message", "hello");
//
// Return: [Socket]
func (s *Socket) Send(args ...any) *Socket {
	s.Emit("message", args...)
	return s
}

// Override `emit`.
// If the event is in `events`, it's emitted normally.
//
// Example:
// socket.emit("hello", "world");
//
// // all serializable datastructures are supported (no need to call JSON.stringify)
// socket.emit("hello", 1, "2", { 3: ["4"], 5: Uint8Array.from([6]) });
//
// // with an acknowledgement from the server
//
//	socket.emit("hello", "world", (val) => {
//	  // ...
//	});
//
// Return: [Socket]
func (s *Socket) Emit(ev string, args ...any) error {
	if RESERVED_EVENTS.Has(ev) {
		return fmt.Errorf(`"%s" is a reserved event name`, ev)
	}

	data := append([]any{ev}, args...)
	data_len := len(data)

	flags := s.flags.Load()

	if s._opts.Retries() > 0 && !flags.FromQueue && !flags.Volatile {
		s._addToQueue(data)
		return nil
	}

	packet := &Packet{
		Packet: &parser.Packet{
			Type: parser.EVENT,
			Data: data,
		},
		Options: &packet.Options{
			Compress: flags.Compress,
		},
	}

	// event ack callback
	if ack, withAck := data[data_len-1].(socket.Ack); withAck {
		id := s.ids.Add(1) - 1
		socket_log.Debug("emitting packet with ack id %d", id)

		packet.Data = data[:data_len-1]
		s._registerAckCallback(id, ack)
		packet.Id = &id
	}

	isTransportWritable := false
	if engine := s.io.engine; engine != nil {
		if transport := engine.Transport(); transport != nil {
			isTransportWritable = transport.Writable()
		}
	}

	isConnected := false
	if s.connected.Load() {
		if engine := s.io.engine; engine != nil && !engine.HasPingExpired() {
			isConnected = true
		}
	}

	if flags.Volatile && !isTransportWritable {
		socket_log.Debug("discard packet as the transport is not currently writable")
	} else if isConnected {
		s.notifyOutgoingListeners(packet)
		s.packet(packet)
	} else {
		s.sendBuffer.Push(packet)
	}

	s.flags.Store(&Flags{})

	return nil
}

func (s *Socket) _registerAckCallback(id uint64, ack socket.Ack) {
	timeout := s.flags.Load().Timeout
	if timeout == nil {
		timeout = s._opts.GetRawAckTimeout()
	}
	if timeout == nil {
		s.acks.Store(id, ack)
		return
	}

	timer := utils.SetTimeout(func() {
		s.acks.Delete(id)
		s.sendBuffer.RemoveAll(func(p *Packet) bool {
			if p.Id != nil && *p.Id == id {
				socket_log.Debug("removing packet with ack id %d from the buffer", id)
				return true
			}
			return false
		})
		socket_log.Debug("event with ack id %d has timed out after %d ms", id, timeout)
		ack(nil, errors.New("operation has timed out"))
	}, *timeout)

	s.acks.Store(id, func(data []any, err error) {
		utils.ClearTimeout(timer)
		ack(data, err)
	})
}

// Emits an event and waits for an acknowledgement
//
// Example:
// // without timeout
// const response = await socket.emitWithAck("hello", "world");
//
// // with a specific timeout
//
//	try {
//	  const response = await socket.timeout(1000).emitWithAck("hello", "world");
//	} catch (err) {
//
//	  // the server did not acknowledge the event in the given delay
//	}
//
// Return: a Promise that will be fulfilled when the server acknowledges the event
func (s *Socket) EmitWithAck(ev string, args ...any) func(socket.Ack) {
	return func(ack socket.Ack) {
		s.Emit(ev, append(args, ack)...)
	}
}

// Add the packet to the queue.
//
// Param: args
func (s *Socket) _addToQueue(args []any) {
	args_len := len(args)
	ack, withAck := args[args_len-1].(socket.Ack)
	if withAck {
		args = args[:args_len-1]
	}

	packet := &QueuedPacket{
		Id:    s._queueSeq.Add(1) - 1,
		Flags: s.flags.Load(),
	}

	args = append(args, func(responseArgs []any, err error) {
		if q, err := s._queue.Get(0); err != nil || packet != q {
			// the packet has already been acknowledged
			return
		}
		if err != nil {
			if tryCount := packet.TryCount.Load(); float64(tryCount) > s._opts.Retries() {
				socket_log.Debug("packet [%d] is discarded after %d tries", packet.Id, tryCount)
				s._queue.Shift()
				if ack != nil {
					ack(nil, err)
				}
			}
		} else {
			socket_log.Debug("packet [%d] was successfully sent", packet.Id)
			s._queue.Shift()
			if ack != nil {
				ack(responseArgs, nil)
			}
		}
		packet.Pending.Store(false)
		s._drainQueue(false)
	})

	packet.Args = args

	s._queue.Push(packet)
	s._drainQueue(false)
}

// Send the first packet of the queue, and wait for an acknowledgement from the server.
//
// Param: force - whether to resend a packet that has not been acknowledged yet
func (s *Socket) _drainQueue(force bool) {
	socket_log.Debug("draining queue")
	if !s.connected.Load() || s._queue.Len() == 0 {
		return
	}
	packet, err := s._queue.Get(0)
	if err != nil {
		return
	}

	if !force && packet.Pending.Load() {
		socket_log.Debug("packet [%d] has already been sent and is waiting for an ack", packet.Id)
		return
	}
	packet.Pending.Store(true)
	packet.TryCount.Add(1)
	socket_log.Debug("sending packet [%d] (try n°%d)", packet.Id, packet.TryCount.Load())
	s.flags.Store(packet.Flags)
	s.Emit(packet.Args[0].(string), packet.Args[1:]...)
}

// Sends a packet.
//
// Param: packet
func (s *Socket) packet(packet *Packet) {
	packet.Nsp = s.nsp
	s.io._packet(packet)
}

// Called upon engine `open`.
func (s *Socket) onopen(...any) {
	socket_log.Debug("transport is open - connecting")
	s._sendConnectPacket(s.auth)
}

// Sends a CONNECT packet to initiate the Socket.IO session.
//
// Param: data
func (s *Socket) _sendConnectPacket(data map[string]any) {
	if s._pid != "" {
		if data == nil {
			data = map[string]any{}
		}
		data["pid"] = s._pid
		data["offset"] = s._lastOffset
	}
	s.packet(&Packet{
		Packet: &parser.Packet{
			Type: parser.CONNECT,
			Data: data,
		},
	})
}

// Called upon engine or manager `error`.
//
// Param: err
func (s *Socket) onerror(errs ...any) {
	if !s.connected.Load() {
		s.EventEmitter.Emit("connect_error", errs...)
	}
}

// Called upon engine `close`.
//
// Param: reason
// Param: description
func (s *Socket) onclose(reason string, description error) {
	socket_log.Debug("close (%s)", reason)
	s.connected.Store(false)
	s.id.Store(nil)
	s.EventEmitter.Emit("disconnect", reason, description)
	s._clearAcks()
}

// Clears the acknowledgement handlers upon disconnection, since the client will never receive an acknowledgement from
// the server.
func (s *Socket) _clearAcks() {
	s.acks.Range(func(id uint64, ack socket.Ack) bool {
		isBuffered := false
		s.sendBuffer.FindIndex(func(packet *Packet) bool {
			if packet.Id != nil && *packet.Id == id {
				isBuffered = true
			}
			return isBuffered
		})
		if !isBuffered {
			s.acks.Delete(id)
			ack(nil, errors.New("socket has been disconnected"))
		}
		return true
	})
}

// Called with socket packet.
//
// Param: packet
func (s *Socket) onpacket(packet *parser.Packet) {
	if packet.Nsp != s.nsp {
		return
	}

	switch packet.Type {
	case parser.CONNECT:
		data, _ := packet.Data.(map[string]any)
		handshake, err := processHandshake(data)
		if err != nil || handshake.Sid == "" {
			s.EventEmitter.Emit(
				"connect_error",
				errors.New(
					"It seems you are trying to reach a Socket.IO server in v2.x with a v3.x client, but they are not compatible (more information here: https://socket.io/docs/v3/migrating-from-2-x-to-3-0/)",
				),
			)
		}
		s.onconnect(handshake.Sid, handshake.Pid)

	case parser.EVENT, parser.BINARY_EVENT:
		s.onevent(packet)

	case parser.ACK, parser.BINARY_ACK:
		s.onack(packet)

	case parser.DISCONNECT:
		s.ondisconnect()

	case parser.CONNECT_ERROR:
		s.destroy()
		data, _ := packet.Data.(map[string]any)
		extendedError, err := processExtendedError(data)
		if err != nil {
			s.EventEmitter.Emit("connect_error", err)
			return
		}
		s.EventEmitter.Emit("connect_error", extendedError.Err())
	}
}

// Called upon a server event.
//
// Param: packet
func (s *Socket) onevent(packet *parser.Packet) {
	args := packet.Data.([]any)
	socket_log.Debug("emitting event %v", args)

	if nil != packet.Id {
		socket_log.Debug("attaching ack callback to event")
		args = append(args, s.ack(*packet.Id))
	}

	if s.connected.Load() {
		s.emitEvent(args)
	} else {
		s.receiveBuffer.Push(args)
	}
}

func (s *Socket) emitEvent(args []any) {
	for _, listener := range s._anyListeners.All() {
		listener(args...)
	}
	s.EventEmitter.Emit(events.EventName(args[0].(string)), args[1:]...)
	if s._pid != "" {
		if args_len := len(args); args_len > 0 {
			if lastOffset, ok := args[args_len-1].(string); ok {
				s._lastOffset = lastOffset
			}
		}
	}
}

// Produces an ack callback to emit with an event.
func (s *Socket) ack(id uint64) socket.Ack {
	sent := &sync.Once{}
	return func(args []any, _ error) {
		// prevent double callbacks
		sent.Do(func() {
			socket_log.Debug("sending ack %v", args)
			s.packet(&Packet{
				Packet: &parser.Packet{
					Type: parser.ACK,
					Id:   &id,
					Data: args,
				},
			})
		})
	}
}

// Called upon a server acknowledgement.
//
// Param: packet
func (s *Socket) onack(packet *parser.Packet) {
	if packet.Id == nil {
		socket_log.Debug("bad ack nil")
		return
	}
	ack, ok := s.acks.Load(*packet.Id)
	if !ok {
		socket_log.Debug("bad ack %d", *packet.Id)
		return
	}
	s.acks.Delete(*packet.Id)
	socket_log.Debug("calling ack %d with %v", *packet.Id, packet.Data)
	ack(packet.Data.([]any), nil)
}

// Called upon server connect.
func (s *Socket) onconnect(id string, pid string) {
	socket_log.Debug("socket connected with id %s", id)
	s.id.Store(id)
	s.recovered.Store(pid != "" && s._pid == pid)
	s._pid = pid // defined only if connection state recovery is enabled
	s.connected.Store(true)
	s.emitBuffered()
	s.EventEmitter.Emit("connect")
	s._drainQueue(true)
}

// Emit buffered events (received and emitted).
func (s *Socket) emitBuffered() {
	s.receiveBuffer.DoWrite(func(values [][]any) [][]any {
		for _, args := range values {
			s.emitEvent(args)
		}
		return values[:0]
	})

	s.sendBuffer.DoWrite(func(packets []*Packet) []*Packet {
		for _, packet := range packets {
			s.notifyOutgoingListeners(packet)
			s.packet(packet)
		}
		return packets[:0]
	})
}

// Called upon server disconnect.
func (s *Socket) ondisconnect() {
	socket_log.Debug("server disconnect (%s)", s.nsp)
	s.destroy()
	s.onclose("io server disconnect", nil)
}

// Called upon forced client/server side disconnections,
// this method ensures the manager stops tracking us and
// that reconnections don't get triggered for s.
func (s *Socket) destroy() {
	if subs := s.subs.Load(); subs != nil {
		// clean subscriptions to avoid reconnections
		subs.DoWrite(func(subDestroys []func()) []func() {
			for _, subDestroy := range subDestroys {
				subDestroy()
			}
			return subDestroys[:0]
		})
		s.subs.Store(nil)
	}
	s.io._destroy(s)
}

// Disconnects the socket manually. In that case, the socket will not try to reconnect.
//
// If this is the last active Socket instance of the {@link Manager}, the low-level connection will be closed.
//
// Example:
// const socket = io();
//
//	socket.on("disconnect", (reason) => {
//	  // console.log(reason); prints "io client disconnect"
//	});
//
// socket.disconnect();
//
// Return: [Socket]
func (s *Socket) Disconnect() *Socket {
	if s.connected.Load() {
		socket_log.Debug("performing disconnect (%s)", s.nsp)
		s.packet(&Packet{Packet: &parser.Packet{Type: parser.DISCONNECT}})
	}

	// remove socket from pool
	s.destroy()

	if s.connected.Load() {
		// fire events
		s.onclose("io client disconnect", nil)
	}
	return s
}

// Alias for [Socket.Disconnect].
//
// Return: [Socket]
func (s *Socket) Close() *Socket {
	return s.Disconnect()
}

// Sets the compress flag.
//
// Example:
//
//	socket.Compress(false).Emit("hello");
//
// Param: compress - if `true`, compresses the sending data
// Return: [Socket]
func (s *Socket) Compress(compress bool) *Socket {
	s.flags.Load().Compress = compress
	return s
}

// Sets a modifier for a subsequent event emission that the event message will be dropped when this socket is not
// ready to send messages.
//
// Example:
//
//	socket.Volatile().Emit("hello"); // the server may or may not receive it
//
// Returns: [Socket]
func (s *Socket) Volatile() *Socket {
	s.flags.Load().Volatile = true
	return s
}

// Sets a modifier for a subsequent event emission that the callback will be called with an error when the
// given number of milliseconds have elapsed without an acknowledgement from the server:
//
// Example:
//
//	socket.Timeout(5000*time.Millisecond).Emit("my-event", func([]any, err) {
//	  if err != nil {
//	    // the server did not acknowledge the event in the given delay
//	  }
//	})
//
// Returns: [Socket]
func (s *Socket) Timeout(timeout time.Duration) *Socket {
	s.flags.Load().Timeout = &timeout
	return s
}

// Adds a listener that will be fired when any event is emitted. The event name is passed as the first argument to the
// callback.
//
// Example:
//
//	socket.OnAny(func(...any) => {
//	  console.log(`got ${event}`);
//	});
//
// Param: listener
func (s *Socket) OnAny(listener events.Listener) *Socket {
	s._anyListeners.Push(listener)
	return s
}

// Adds a listener that will be fired when any event is emitted. The event name is passed as the first argument to the
// callback. The listener is added to the beginning of the listeners array.
//
// Example:
//
//	socket.prependAny((event, ...args) => {
//	  console.log(`got event ${event}`);
//	});
//
// Param: listener
func (s *Socket) PrependAny(listener events.Listener) *Socket {
	s._anyListeners.Unshift(listener)
	return s
}

// Removes the listener that will be fired when any event is emitted.
//
// Example:
//
//	const catchAllListener = (event, ...args) => {
//	  console.log(`got event ${event}`);
//	}
//
// socket.onAny(catchAllListener);
//
// // remove a specific listener
// socket.offAny(catchAllListener);
//
// // or remove all listeners
// socket.offAny();
//
// Param: listener
func (s *Socket) OffAny(listener events.Listener) *Socket {
	if listener != nil {
		listenerPointer := reflect.ValueOf(listener).Pointer()
		s._anyListeners.RangeAndSplice(func(_listener events.Listener, i int) (bool, int, int, []events.Listener) {
			return reflect.ValueOf(listener).Pointer() == listenerPointer, i, 1, nil
		})
	} else {
		s._anyListeners.Clear()
	}
	return s
}

// Returns an array of listeners that are listening for any event that is specified. This array can be manipulated,
// e.g. to remove listeners.
func (s *Socket) ListenersAny() []events.Listener {
	return s._anyListeners.All()
}

// Adds a listener that will be fired when any event is emitted. The event name is passed as the first argument to the
// callback.
//
// Note: acknowledgements sent to the server are not included.
//
// Example:
//
//	socket.onAnyOutgoing((event, ...args) => {
//	  console.log(`sent event ${event}`);
//	});
//
// Param: listener
func (s *Socket) OnAnyOutgoing(listener events.Listener) *Socket {
	s._anyOutgoingListeners.Push(listener)
	return s
}

// Adds a listener that will be fired when any event is emitted. The event name is passed as the first argument to the
// callback. The listener is added to the beginning of the listeners array.
//
// Note: acknowledgements sent to the server are not included.
//
// Example:
// socket.prependAnyOutgoing((event, ...args) => {
//   console.log(`sent event ${event}`);
// });
//
// Param: listener

func (s *Socket) PrependAnyOutgoing(listener events.Listener) *Socket {
	s._anyOutgoingListeners.Unshift(listener)
	return s
}

// Removes the listener that will be fired when any event is emitted.
//
// Example:
//
//	const catchAllListener = (event, ...args) => {
//	  console.log(`sent event ${event}`);
//	}
//
// socket.onAnyOutgoing(catchAllListener);
//
// // remove a specific listener
// socket.offAnyOutgoing(catchAllListener);
//
// // or remove all listeners
// socket.offAnyOutgoing();
//
// Param: [listener] - the catch-all listener (optional)
func (s *Socket) OffAnyOutgoing(listener events.Listener) *Socket {
	if listener != nil {
		listenerPointer := reflect.ValueOf(listener).Pointer()
		s._anyOutgoingListeners.RangeAndSplice(func(_listener events.Listener, i int) (bool, int, int, []events.Listener) {
			return reflect.ValueOf(listener).Pointer() == listenerPointer, i, 1, nil
		})
	} else {
		s._anyOutgoingListeners.Clear()
	}
	return s
}

// Returns an array of listeners that are listening for any event that is specified. This array can be manipulated,
// e.g. to remove listeners.
func (s *Socket) ListenersAnyOutgoing() []events.Listener {
	return s._anyOutgoingListeners.All()
}

// Notify the listeners for each packet sent
//
// Param: packet
func (s *Socket) notifyOutgoingListeners(packet *Packet) {
	if s._anyOutgoingListeners.Len() > 0 {
		for _, listener := range s._anyOutgoingListeners.All() {
			if args, ok := packet.Data.([]any); ok {
				listener(args...)
			} else {
				listener(packet.Data)
			}
		}
	}
}
