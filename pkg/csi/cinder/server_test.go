package cinder

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestServerReleaseWaitAfterStopCalling(t *testing.T) {
	defer goleak.VerifyNone(t)

	var (
		server = NewNonBlockingGRPCServer()
		ch     = make(chan struct{})
	)
	server.Start(FakeEndpoint, nil, nil, nil)

	go func() {
		server.Wait()
	}()

	_, address, err := ParseEndpoint(FakeEndpoint)
	require.NoError(t, err)

	// this loop is needed to wait for the server start up
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			require.Fail(t, "server does not started")
		default:
		}

		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err != nil {
			continue
		}
		if conn == nil {
			continue
		}
		_ = conn.Close()
		break
	}

	go func() {
		server.Stop()
		close(ch)
	}()

	<-ch
}
