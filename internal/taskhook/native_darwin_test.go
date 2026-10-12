package taskhook

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestDarwinProbeFailureAndBoundedOutputCannotAttestOwnership(t *testing.T) {
	original := probeInfo
	defer func() { probeInfo = original }()
	probeInfo = func(string, ...int) (*unix.KinfoProc, error) { return nil, unix.EIO }
	_, err := platformProcess(10)
	require.ErrorIs(t, err, ErrUnsupported)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = platformFiles(ctx, 10)
	require.ErrorIs(t, err, ErrUnsupported)
	var out limitedOutput
	n, err := out.Write(bytes.Repeat([]byte("x"), 256<<10))
	require.NoError(t, err)
	require.Equal(t, 256<<10, n)
	n, err = out.Write([]byte("x"))
	require.ErrorIs(t, err, ErrLimit)
	require.Zero(t, n)
	require.Equal(t, 256<<10, out.Len())
}

func TestDarwinOpenSetParserLimitsAndMissingIdentity(t *testing.T) {
	for _, count := range []int{MaxHeldFiles, MaxHeldFiles + 1} {
		var data bytes.Buffer
		for i := 0; i < count; i++ {
			fmt.Fprintf(&data, "f%d\x00D1\x00i%d\x00n/workspace/rollout-%d.jsonl\x00", i, i, i)
		}
		files, err := parseLsof(data.String())
		if count == MaxHeldFiles {
			require.NoError(t, err)
			require.Len(t, files, count)
		} else {
			require.Error(t, err)
		}
	}
	for _, data := range []string{"f0\x00Dinvalid\x00i1\x00n/workspace/rollout-a.jsonl\x00",
		"f0\x00D1\x00iinvalid\x00n/workspace/rollout-a.jsonl\x00",
		"f0\x00Dinvalid\x00i1\x00n/workspace/rollout-a.jsonl\x00f1\x00"} {
		_, err := parseLsof(data)
		require.Error(t, err)
	}
	files, err := parseLsof("f0\x00Dinvalid\x00i1\x00n/not-a-rollout\x00")
	require.NoError(t, err)
	require.Empty(t, files)
}
