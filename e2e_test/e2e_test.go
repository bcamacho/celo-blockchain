package e2e_test

import (
	"testing"
	"time"

	"github.com/celo-org/celo-blockchain/test"
	"github.com/stretchr/testify/require"
)

func TestFunction(t *testing.T) {
	now := time.Now()
	println("starting test", now.String())
	_, err := test.NewNetworkFromUsers(23456)
	require.NoError(t, err)
	println("test took", time.Since(now).String())
}
