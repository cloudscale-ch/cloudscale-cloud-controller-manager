package cloudscale

import (
	"errors"
	"os"
	"testing"
	"testing/iotest"
	"time"
)

func TestMaskAccessToken(t *testing.T) {
	testCases := []struct {
		label    string
		input    string
		expected string
	}{
		{
			"Empty Access Token",
			"",
			"",
		},
		{
			"Secret Access Token",
			"12345789abcdefghijklmnopqrstuvwx",
			"1234****************************",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			r := maskAccessToken(tc.input)
			if r != tc.expected {
				t.Errorf("expected %s for %s, got %s", tc.expected, tc.input, r)
			}
		})
	}
}

func TestNewCloudscaleProviderWithoutToken(t *testing.T) {
	if _, err := newCloudscaleProvider(nil); err == nil {
		t.Error("no token in env: newCloudscaleProvider should have failed")
	}
}

func TestNewCloudscaleProviderWithToken(t *testing.T) {
	os.Setenv(AccessToken, "1234")
	if _, err := newCloudscaleProvider(nil); err != nil {
		t.Error("newCloudscaleProvider should initialize with just a token")
	}
}

func TestNewCloudscaleProviderWithBadConfig(t *testing.T) {
	cfg := iotest.ErrReader(errors.New("bad config"))
	if _, err := newCloudscaleProvider(cfg); err != nil {
		t.Error("newCloudscaleProvider should ignore the config file")
	}
}

func TestNewCloudscaleProviderWithDefaultTimeout(t *testing.T) {
	provider, _ := newCloudscaleProvider(nil)
	if cs, _ := provider.(*cloud); cs.timeout != (5 * time.Second) {
		t.Errorf("unexpected default timeout: %s", cs.timeout)
	}
}

func TestNewCloudscaleProviderWithInvalidTimeout(t *testing.T) {
	os.Setenv(ApiTimeout, "asdf")
	provider, _ := newCloudscaleProvider(nil)
	if cs, _ := provider.(*cloud); cs.timeout != (5 * time.Second) {
		t.Errorf("unexpected fallback timeout: %s", cs.timeout)
	}
}

func TestNewCloudscaleProviderWithCustomTimeout(t *testing.T) {
	os.Setenv(ApiTimeout, "10")
	provider, _ := newCloudscaleProvider(nil)
	if cs, _ := provider.(*cloud); cs.timeout != (10 * time.Second) {
		t.Errorf("ignored %s: %s", ApiTimeout, cs.timeout)
	}
}
