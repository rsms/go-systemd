// Copyright 2020 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

// ErrNoNotificationSocket is returned when NOTIFY_SOCKET is not set in the environment
var ErrNoNotificationSocket = errors.New("notification socket not available")

// SdNotifyBarrier allows the caller to synchronize against reception of
// previously sent notification messages and uses the "BARRIER=1" command.
//
// If `unsetEnvironment` is true, the environment variable `NOTIFY_SOCKET`
// will be unconditionally unset.
//
// This feature was added in systemd v246
func SdNotifyBarrier(ctx context.Context, unsetEnvironment bool) error {
	// modelled after libsystemd's sd_notify_barrier

	// construct unix socket address from systemd environment variable
	socketAddr := &net.UnixAddr{
		Name: os.Getenv("NOTIFY_SOCKET"),
		Net:  "unixgram",
	}
	if socketAddr.Name == "" {
		return ErrNoNotificationSocket
	}

	// create a pipe for communicating with systemd daemon
	pipe_r, pipe_w, err := os.Pipe() // (r *File, w *File, error)
	if err != nil {
		return err
	}

	if unsetEnvironment {
		if err := os.Unsetenv("NOTIFY_SOCKET"); err != nil {
			return err
		}
	}

	// connect to unix socket at socketAddr
	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	// get the FD for the unix socket file
	connf, err := conn.File()
	if err != nil {
		return err
	}

	// send over write end of the pipe to the systemd daemon
	rights := syscall.UnixRights(int(pipe_w.Fd()))
	err = syscall.Sendmsg(int(connf.Fd()), []byte("BARRIER=1"), rights, nil, 0)
	if err != nil {
		return err
	}
	pipe_w.Close()

	// wait for systemd to close the pipe
	ctxch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			pipe_r.SetReadDeadline(time.Now())
		case <-ctxch:
		}
	}()
	var b [1]byte
	_, err = pipe_r.Read(b[:])
	close(ctxch)
	if err == io.EOF {
		err = nil
	}

	return err
}
