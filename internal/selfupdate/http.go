package selfupdate

import (
	"net/http"
	"net/url"
)

func httpClientWithProxy(opts *FetchOptions) *http.Client {
	if opts == nil || opts.Proxy == "" {
		return http.DefaultClient
	}
	proxyURL, err := url.Parse(opts.Proxy)
	if err != nil {
		return http.DefaultClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	return &http.Client{
		Transport: transport,
	}
}
