package main

import (
	"fmt"
	"io"
	"net"
	"time"
	"unicode/utf8"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipeName = `\\.\pipe\LoTranslationPreview-v1`

type inputEvent struct {
	kind   byte
	text   string
	target inputTarget
}

func listenToRime() (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", user.User.Sid.String()),
		InputBufferSize:    8192,
		OutputBufferSize:   4096,
	})
}

func readRime(listener net.Listener, events chan<- inputEvent, done <-chan struct{}) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		target := foregroundTarget()
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		data, err := io.ReadAll(io.LimitReader(conn, 4098))
		_ = conn.Close()
		if err != nil || len(data) == 0 || len(data) > 4097 || !utf8.Valid(data[1:]) {
			continue
		}
		if data[0] != 'T' && (len(data) != 1 || (data[0] != 'B' && data[0] != 'R')) {
			continue
		}
		select {
		case events <- inputEvent{data[0], string(data[1:]), target}:
		case <-done:
			return
		}
	}
}
