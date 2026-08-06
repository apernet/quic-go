package handshake

import (
	"testing"

	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/require"
)

func TestUTLSConnectionStateToStdPreservesECHAccepted(t *testing.T) {
	state := utlsConnectionStateToStd(utls.ConnectionState{ECHAccepted: true})
	require.True(t, state.ECHAccepted)
}
