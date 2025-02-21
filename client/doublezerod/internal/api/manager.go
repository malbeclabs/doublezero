package api

import (
	"context"
	"net"
	"net/http"
)

type ApiServer struct {
	*http.Server
	sockFile string
}

type Option func(*ApiServer)

func NewApiServer(options ...Option) *ApiServer {
	api := &ApiServer{
		Server: &http.Server{},
	}
	for _, o := range options {
		o(api)
	}
	return api
}

func WithSockFile(sockFile string) Option {
	return func(a *ApiServer) {
		a.sockFile = sockFile
	}
}

func WithBaseContext(ctx context.Context) Option {
	return func(a *ApiServer) {
		a.BaseContext = func(net.Listener) context.Context { return ctx }
	}
}

func WithHandler(mux *http.ServeMux) Option {
	return func(a *ApiServer) {
		a.Handler = mux
	}
}
