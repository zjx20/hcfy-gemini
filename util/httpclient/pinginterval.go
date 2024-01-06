package httpclient

import (
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

func CustomPingInterval(interval time.Duration) *http.Client {
	t1 := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	// make http2.Transport use proxy
	t2, err := http2.ConfigureTransports(t1)
	if err != nil {
		panic(err)
	}
	t2.ReadIdleTimeout = interval
	return &http.Client{
		Transport: t1,
	}
}
