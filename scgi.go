// Package scgi provides a simple scgi client and a number of primitives needed
// for basic scgi operation.
//
// There are two main ways to use this package. It can be used directly as a
// net/http.Client's RoundTripper or it can be added to a net/http.Transport
// using RegisterProtocol.
package scgi

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// WriteNetstring takes the given data and writes it in netstring format to the
// given writer. It does not do any validation on the actual data.
func WriteNetstring(w io.Writer, data []byte) error {
	_, err := w.Write([]byte(strconv.Itoa(len(data))))
	if err != nil {
		return fmt.Errorf("netstring: write error: %v", err)
	}

	_, err = w.Write([]byte{':'})
	if err != nil {
		return fmt.Errorf("netstring: write error: %v", err)
	}

	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("netstring: write error: %v", err)
	}

	_, err = w.Write([]byte{','})
	if err != nil {
		return fmt.Errorf("netstring: write error: %v", err)
	}

	return nil
}

// ReadNetstring assumes the next thing arriving from a bufio.Reader is a
// netstring and attempts to read/parse it.
func ReadNetstring(r *bufio.Reader) (string, error) {
	dataLen, err := r.ReadString(':')
	if err != nil {
		return "", fmt.Errorf("netstring: read error: %v", err)
	}

	// Chop off the trailing ":"
	dataLen = dataLen[:len(dataLen)-1]
	count, err := strconv.Atoi(dataLen)
	if err != nil {
		return "", fmt.Errorf("netstring: read error: %v", err)
	}

	data := make([]byte, count+1)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return "", fmt.Errorf("netstring: read error: %v", err)
	}

	if data[len(data)-1] != ',' {
		return "", errors.New("netstring: read error: missing trailing comma")
	}
	data = data[:len(data)-1]

	return string(data), nil
}

// Client is an implementation of net/http.RoundTripper which supports SCGI
// sockets.
//
// This client supports three different types of urls:
//
//   - Relative socket path (scgi:///relative/path)
//   - Absolute socket path (scgi:////absolute/path)
//   - Host/Port (scgi://host:port)
type Client struct{}

// RoundTrip implements the net/http.RoundTripper interface.
func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	if (req.URL.Host != "" && req.URL.Path != "") || (req.URL.Host == "" && req.URL.Path == "") {
		return nil, errors.New("scgi: round trip: invalid scgi connection string")
	}

	var data []byte
	var err error
	if req.Body != nil {
		data, err = ioutil.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("scgi: round trip: body read error: %v", err)
		}
	}

	var scgiConn net.Conn
	if req.URL.Host == "" {
		// Chop off the first slash so it's possible to support relative paths.
		path := req.URL.Path
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		scgiConn, err = net.Dial("unix", req.URL.Path)
		if err != nil {
			return nil, fmt.Errorf("scgi: round trip over unix socket: %v", err)
		}
	} else {
		host := req.URL.Hostname()
		port := req.URL.Port()
		if port == "" {
			port = "80"
		}
		scgiConn, err = net.Dial("tcp", host+":"+port)
		if err != nil {
			return nil, fmt.Errorf("scgi: round trip over tcp: %v", err)
		}
	}

	// Write the required SCGI headers
	var headers = []string{
		"CONTENT_LENGTH",
		strconv.Itoa(len(data)),
		"SCGI",
		"1",
		"REQUEST_METHOD",
		req.Method,
		"SERVER_PROTOCOL",
		req.Proto,
	}

	headerBuf := &bytes.Buffer{}
	for _, val := range headers {
		headerBuf.WriteString(val)
		headerBuf.Write([]byte{0x00})
	}

	// Write additional headers
	for key, val := range req.Header {
		headerBuf.WriteString(strings.ToUpper(key))
		headerBuf.Write([]byte{0x00})
		headerBuf.WriteString(strings.Join(val, ","))
		headerBuf.Write([]byte{0x00})
	}

	err = WriteNetstring(scgiConn, headerBuf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("scgi: round trip: %v", err)
	}

	_, err = scgiConn.Write(data)
	if err != nil {
		return nil, fmt.Errorf("scgi: round trip write error: %v", err)
	}

	// There isn't a method for cgi reponse parsing, but they're close enough
	// that we can hack on what's needed and use a normal http parser. This does
	// assume that the Status header is sent first, but in my experience most
	// implementations do this anyway.
	scgiRead := bufio.NewReader(scgiConn)

	// Grab the first line and chop off the extra characters from the end.
	firstLine, err := scgiRead.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("scgi: round trip: invalid format: %v", err)
	}
	if firstLine[len(firstLine)-1] == '\n' {
		firstLine = firstLine[:len(firstLine)-1]
	}
	if firstLine[len(firstLine)-1] == '\r' {
		firstLine = firstLine[:len(firstLine)-1]
	}

	// The first line should be a header containing "Status: 200 OK". We chop it
	// in half, ensure this is the Status header, and use the second part in the
	// http response.
	parts := strings.SplitN(firstLine, ": ", 2)
	if len(parts) != 2 {
		return nil, errors.New("scgi: round trip: invalid status response format")
	}
	if parts[0] != "Status" {
		return nil, errors.New("scgi: round trip: invalid status header")
	}

	scgiRead = bufio.NewReader(
		io.MultiReader(
			bytes.NewBufferString(req.Proto+" "+parts[1]+"\r\n"),
			scgiRead))

	resp, err := http.ReadResponse(scgiRead, req)
	if err != nil {
		return nil, errors.New("scgi: round trip")
	}

	return resp, nil
}
