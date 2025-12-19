package testkit

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type HelloResponse struct {
	ServerAddress string
	ServerName    string
	Date          time.Time
	URI           string
	RequestID     string
}

func HelloNginx(addr string, port uint16) (*HelloResponse, error) {
	body, err := HTTPRead("http://" + net.JoinHostPort(addr, strconv.FormatUint(
		uint64(port), 10)))
	if err != nil {
		return nil, err
	}

	response := HelloResponse{}
	lines := strings.Split(body, "\n")
	for i := range lines {
		switch {
		case strings.HasPrefix(lines[i], "Server address: "):
			response.ServerAddress = strings.SplitN(lines[i], ": ", 2)[1]
		case strings.HasPrefix(lines[i], "Server name: "):
			response.ServerName = strings.SplitN(lines[i], ": ", 2)[1]
		case strings.HasPrefix(lines[i], "URI: "):
			response.URI = strings.SplitN(lines[i], ": ", 2)[1]
		case strings.HasPrefix(lines[i], "Request ID: "):
			response.RequestID = strings.SplitN(lines[i], ": ", 2)[1]
		case strings.HasPrefix(lines[i], "Date: "):
			data := strings.SplitN(lines[i], ": ", 2)[1]
			response.Date, err = time.Parse("02/Jan/2006:15:04:05 -0700", data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", data, err)
			}
		}
	}

	return &response, nil
}

func HTTPRead(url string) (string, error) {
	// Disable keep-alive, as that works around round-robin
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true

	client := http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}

	return string(body), nil
}
