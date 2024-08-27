// A http echo server to get information about connections made to it.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
)

func main() {
	host := flag.String("host", "127.0.0.1", "Host to connect to")
	port := flag.Int("port", 8080, "Port to connect to")

	flag.Parse()

	serve(*host, *port)
}

// log http requests in basic fashion
func log(h http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
			fmt.Printf("%s %s (%s)\n", r.Method, r.RequestURI, r.RemoteAddr)
		})
}

// serve HTTP API on the given host and port
func serve(host string, port int) {
	router := http.NewServeMux()

	// Returns 'true' if the PROXY protocol was used for the given connection
	router.HandleFunc("GET /proxy-protocol/used",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, r.Context().Value("HasProxyHeader"))
		})

	addr := fmt.Sprintf("%s:%d", host, port)

	server := http.Server{
		Addr:    addr,
		Handler: log(router),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			hasProxyHeader := false

			if c, ok := c.(*proxyproto.Conn); ok {
				hasProxyHeader = c.ProxyHeader() != nil
			}

			return context.WithValue(ctx, "HasProxyHeader", hasProxyHeader)
		},
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Listening on %s\n", addr)

	proxyListener := &proxyproto.Listener{
		Listener:          listener,
		ReadHeaderTimeout: 10 * time.Second,
	}
	defer proxyListener.Close()

	server.Serve(proxyListener)
}
