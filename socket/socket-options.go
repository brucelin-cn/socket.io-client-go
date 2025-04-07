package socket

import "time"

type (
	SocketOptionsInterface interface {
		SetAuth(map[string]any)
		GetRawAuth() map[string]any
		Auth() map[string]any

		SetRetries(float64)
		GetRawRetries() *float64
		Retries() float64

		SetAckTimeout(time.Duration)
		GetRawAckTimeout() *time.Duration
		AckTimeout() time.Duration
	}

	SocketOptions struct {
		// the authentication payload sent when connecting to the Namespace
		auth map[string]any

		// The maximum number of retries. Above the limit, the packet will be discarded.
		//
		// Using `Infinity` means the delivery guarantee is "at-least-once" (instead of "at-most-once" by default), but a
		// smaller value like 10 should be sufficient in practice.
		retries *float64

		// The default timeout in milliseconds used when waiting for an acknowledgement.
		ackTimeout *time.Duration
	}
)

func DefaultSocketOptions() *SocketOptions {
	return &SocketOptions{}
}

func (s *SocketOptions) Assign(data SocketOptionsInterface) SocketOptionsInterface {
	if data == nil {
		return s
	}

	if data.GetRawAuth() != nil {
		s.SetAuth(data.Auth())
	}
	if data.GetRawRetries() != nil {
		s.SetRetries(data.Retries())
	}
	if data.GetRawAckTimeout() != nil {
		s.SetAckTimeout(data.AckTimeout())
	}

	return s
}

func (s *SocketOptions) SetAuth(auth map[string]any) {
	s.auth = auth
}
func (s *SocketOptions) GetRawAuth() map[string]any {
	return s.auth
}
func (s *SocketOptions) Auth() map[string]any {
	return s.auth
}

func (s *SocketOptions) SetRetries(retries float64) {
	s.retries = &retries
}
func (s *SocketOptions) GetRawRetries() *float64 {
	return s.retries
}
func (s *SocketOptions) Retries() float64 {
	if retries := s.retries; retries != nil {
		return *retries
	}

	return 0
}

func (s *SocketOptions) SetAckTimeout(ackTimeout time.Duration) {
	s.ackTimeout = &ackTimeout
}
func (s *SocketOptions) GetRawAckTimeout() *time.Duration {
	return s.ackTimeout
}
func (s *SocketOptions) AckTimeout() time.Duration {
	if ackTimeout := s.ackTimeout; ackTimeout != nil {
		return *ackTimeout
	}

	return 0
}
