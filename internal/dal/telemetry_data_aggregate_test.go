package dal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAggregateQueryArgsOrderAndWindowUnit(t *testing.T) {
	args := aggregateQueryArgs(TelemetryDatasAggregate{
		STime:           1778311391217,
		ETime:           1778311401217,
		Key:             "test_data1",
		DeviceID:        "e2079484-33a5-43b6-7dd5-0f913c8a2eb4",
		AggregateWindow: 30000,
	})

	require.Equal(t, []interface{}{
		int64(1778311391217),
		int64(1778311401217),
		"test_data1",
		"e2079484-33a5-43b6-7dd5-0f913c8a2eb4",
		int64(30),
		int64(30),
	}, args)
}

func TestAggregateQueryArgsMinimumWindow(t *testing.T) {
	args := aggregateQueryArgs(TelemetryDatasAggregate{AggregateWindow: 500})

	require.Equal(t, int64(1), args[4])
	require.Equal(t, int64(1), args[5])
}
