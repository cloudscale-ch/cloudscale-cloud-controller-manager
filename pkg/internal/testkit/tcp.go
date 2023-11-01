package testkit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
)

func TCPRead(addr string, port int32) (string, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		return "", fmt.Errorf(
			"failed to connect to %s:%d: %w", addr, port, err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf(
			"failed to read from %s:%d: %w", addr, port, err)
	}

	return line, nil
}
