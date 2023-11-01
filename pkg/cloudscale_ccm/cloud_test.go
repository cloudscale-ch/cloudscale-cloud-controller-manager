package cloudscale_ccm

import (
	"errors"
	"os"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMaskAccessToken(t *testing.T) {

	assertMasked := func(input string, expected string) {
		assert.Equal(t, expected, maskAccessToken(input))
	}

	assertMasked("", "")
	assertMasked(
		"12345789abcdefghijklmnopqrstuvwx",
		"1234****************************",
	)
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

func TestDefaultTimeout(t *testing.T) {
	timeout := apiTimeout()
	assert.Equal(t, timeout, 20*time.Second)
}

func TestCustomTimeout(t *testing.T) {
	os.Setenv(ApiTimeout, "5")
	timeout := apiTimeout()
	assert.Equal(t, timeout, 5*time.Second)
}
