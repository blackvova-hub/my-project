package marketdata

import (
	"net/http"
	"time"
)

// Keep connections for the bounded archive pool instead of retaining only
// the default two idle connections per host and repeatedly negotiating TLS.
func httpClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 128
	transport.MaxIdleConnsPerHost = 64
	transport.MaxConnsPerHost = 64
	return &http.Client{Timeout: timeout, Transport: transport}
}
