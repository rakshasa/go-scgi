# scgi

Package scgi provides a simple scgi client and a number of primitives needed
for basic scgi operation.

## Usage

There are two main ways to use this package. It can be used directly as a
net/http.Client's RoundTripper or it can be added to a net/http.Transport
using RegisterProtocol.

```go
t := &http.Transport{}
t.RegisterProtocol("scgi", &scgi.Client{})
client := &http.Client{Transport: t}
req, err := http.NewRequest("GET", "scgi:////run/scgi.sock", nil)
resp, err := client.Do(req)
```

The URI used in the request only specifies the path to the socket file.
To communicate with the server, add CGI parameters as request headers.

```go
req.Header.Set("REMOTE_ADDR", "127.0.0.1")
req.Header.Set("REQUEST_URI", "/resource?foo=bar")
req.Header.Set("QUERY_STRING", "foo=bar")
```
