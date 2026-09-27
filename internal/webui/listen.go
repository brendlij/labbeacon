package webui

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) Listen() (func(context.Context) error, <-chan error, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(s.Settings.BindAddress, strconv.Itoa(s.Settings.Port)))
	if err != nil {
		return nil, nil, err
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		done <- err
	}()
	return server.Shutdown, done, nil
}
