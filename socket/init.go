package socket

import (
	"github.com/zishang520/engine.io-client-go/transports"
	"github.com/zishang520/engine.io/v2/log"
	"github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/socket.io-client-go/utils"
)

var (
	manager_log = log.NewLog("socket.io-client:manager")
	socket_log  = log.NewLog("socket.io-client:socket")
	client_log  = log.NewLog("socket.io-client")

	RESERVED_EVENTS = types.NewSet("connect", "connect_error", "disconnect", "disconnecting", "newListener", "removeListener")

	Polling      = transports.Polling
	WebSocket    = transports.WebSocket
	WebTransport = transports.WebTransport

	cache types.Map[string, *Manager]
)

func init() {
	cache = types.Map[string, *Manager]{}
}

func lookup(uri string, opts OptionsInterface) (*Socket, error) {
	path := "/socket.io"
	if opts.GetRawPath() != nil {
		path = opts.Path()
	}
	parsed, err := utils.Url(uri, path)
	if err != nil {
		return nil, err
	}

	source := parsed.String()
	id := parsed.Id
	sameNamespace := false
	if manager, ok := cache.Load(id); ok {
		_, sameNamespace = manager.nsps.Load(parsed.Path)
	}
	newConnection := opts.ForceNew() || !opts.Multiplex() || sameNamespace

	var io *Manager
	if newConnection {
		client_log.Debug("ignoring socket cache for %s", source)
		io = NewManager(source, opts)
	} else {
		manager, ok := cache.LoadOrStore(id, NewManager(source, opts))
		if !ok {
			client_log.Debug("new io instance for %s", source)
		}
		io = manager
	}
	if opts.GetRawQuery() == nil && parsed.RawQuery != "" {
		opts.SetQuery(parsed.Query())
	}

	return io.Socket(parsed.Path, opts), nil
}

func Io(uri string, opts OptionsInterface) (*Socket, error) {
	return lookup(uri, opts)
}

func Connect(uri string, opts OptionsInterface) (*Socket, error) {
	return lookup(uri, opts)
}
