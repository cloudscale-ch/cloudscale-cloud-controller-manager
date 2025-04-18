package testkit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v6"
)

// MockAPIServer is a mock http server that builds on httptest.Server and
// http.ServeMux and provides methods to easily return mocked cloudscale API
// responses.
type MockAPIServer struct {
	mux      *http.ServeMux
	server   *httptest.Server
	lastsent []byte
}

func NewMockAPIServer() *MockAPIServer {
	return &MockAPIServer{
		mux:    nil,
		server: nil,
	}
}

// On matches the given pattern and returns a status and the given data. The
// data can be a string or anything that go can marshal into a JSON.
//
// The servrer adds a default route that respods with an empty JSON object
// and a 404 status code.
//
// Note, this method has no effect if the server is started, and all registered
// patterns need to be re-applied after the server is stopped using Close.
func (m *MockAPIServer) On(pattern string, status int, data any) {
	if m.mux == nil {
		m.mux = http.NewServeMux()
		m.On("/", 404, "{}")
	}

	m.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)

		if status == 404 {
			fmt.Println("Not handled: {}", r.URL)
		}

		var (
			body []byte
			err  error
		)

		// Turn response data into a JSON
		switch v := data.(type) {
		case string:
			body = []byte(v)
		default:
			body, err = json.Marshal(data)
			if err != nil {
				panic(fmt.Sprintf("failed to %v to json: %s", data, err))
			}
		}

		// Write response data
		if len(body) > 0 {
			_, err = w.Write(body)
			if err != nil {
				panic(fmt.Sprintf(
					"failed to write body for %s: %s", pattern, err))
			}
		}

		// Capture JSON that was sent for PUT/POST
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				panic(fmt.Sprintf("failed read request %s: %s", pattern, err))
			}
			m.lastsent = data
		}
	})
}

// WithServers ensures that the /v1/servers endpoints respond with the
// given server objects. In addition to /v1/servers, this also implements
// /v1/servers/<uuid> for any server with a UUID.
func (m *MockAPIServer) WithServers(servers []cloudscale.Server) {
	m.On("/v1/servers", 200, servers)
	for _, server := range servers {
		if server.UUID != "" {
			m.On("/v1/servers/"+server.UUID, 200, server)
		}
	}
}

// WithLoadBalancers ensures that the /v1/loadbalancers endpoints respond with
// the given loadbalancer objects. In addition to /v1/loadbalancers, this also
// implements /v1/loadbalancers/<uuid> for any loadbalancer with a UUID.
func (m *MockAPIServer) WithLoadBalancers(lbs []cloudscale.LoadBalancer) {
	m.On("/v1/load-balancers", 200, lbs)
	for _, lb := range lbs {
		if lb.UUID != "" {
			m.On("/v1/load-balancers/"+lb.UUID, 200, lb)
		}
	}
}

// Client returns a cloudscale client pointing at the mock API server.
func (m *MockAPIServer) Client() *cloudscale.Client {
	if m.server == nil {
		panic("must call Start() before accessing the client")
	}

	client := cloudscale.NewClient(nil)
	client.BaseURL, _ = url.Parse(m.server.URL)
	client.AuthToken = ""

	return client
}

// LastSent unmarshals the JSON last sent to the API server via POST/PUT/PATCH.
func (m *MockAPIServer) LastSent(v any) {
	err := json.Unmarshal(m.lastsent, v)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal: %s", m.lastsent))
	}
}

// Start runs the server in the background, until it is stopped/closed.
func (m *MockAPIServer) Start() {
	if m.server != nil {
		panic("must call Close() before starting another time")
	}

	m.server = httptest.NewServer(m.mux)
}

// Close stops/closes the server and resets it.
func (m *MockAPIServer) Close() {
	if m.server != nil {
		m.server.Close()
		m.server = nil
		m.mux = nil
	}
}
