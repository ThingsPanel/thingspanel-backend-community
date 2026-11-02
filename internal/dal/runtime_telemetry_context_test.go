package dal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"project/internal/model"
	tptodb "project/third_party/grpc/tptodb_client"
	pb "project/third_party/grpc/tptodb_client/grpc_tptodb"
)

type runtimeBlockingStore struct{ pb.ThingsPanelClient }

func (runtimeBlockingStore) GetDeviceAttributesCurrents(ctx context.Context, _ *pb.GetDeviceAttributesCurrentsRequest, _ ...grpc.CallOption) (*pb.GetDeviceAttributesCurrentsReply, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (runtimeBlockingStore) GetDeviceHistoryWithPageAndPage(ctx context.Context, _ *pb.GetDeviceHistoryWithPageAndPageRequest, _ ...grpc.CallOption) (*pb.GetDeviceHistoryWithPageAndPageReply, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRuntimeTelemetryExternalStoreHonorsRequestDeadline(t *testing.T) {
	oldClient, oldType := tptodb.TptodbClient, viper.Get("grpc.tptodb_type")
	tptodb.TptodbClient = runtimeBlockingStore{}
	viper.Set("grpc.tptodb_type", "TSDB")
	t.Cleanup(func() { tptodb.TptodbClient = oldClient; viper.Set("grpc.tptodb_type", oldType) })
	for _, history := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		done := make(chan error, 1)
		go func() {
			if history {
				_, _, err := GetHistoryTelemetrDataByPage(&model.GetTelemetryHistoryDataByPageReq{DeviceID: "fixture-device"}, ctx)
				done <- err
			} else {
				_, err := GetCurrentTelemetryDataEvolution("fixture-device", ctx)
				done <- err
			}
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("missing deadline propagation: %v", err)
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatal("external telemetry read ignored request deadline")
		}
		cancel()
	}
}
