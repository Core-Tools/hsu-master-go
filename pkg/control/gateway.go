package control

import (
	"context"

	"github.com/core-tools/hsu-master/pkg/domain"
	"github.com/core-tools/hsu-master/pkg/generated/api/proto"
	"github.com/core-tools/hsu-master/pkg/logging"

	"google.golang.org/grpc"
)

func NewGRPCClientGateway(grpcClientConnection grpc.ClientConnInterface, logger logging.Logger) domain.Contract {
	grpcClient := proto.NewMasterServiceClient(grpcClientConnection)
	return &grpcClientGateway{
		grpcClient: grpcClient,
		logger:     logger,
	}
}

type grpcClientGateway struct {
	grpcClient proto.MasterServiceClient
	logger     logging.Logger
}

func (gw *grpcClientGateway) Status(ctx context.Context) (string, error) {
	response, err := gw.grpcClient.Status(ctx, &proto.StatusRequest{})
	if err != nil {
		gw.logger.Errorf("Status client gateway: %v", err)
		return "", err
	}
	gw.logger.Debugf("Status client gateway done")
	return response.Status, nil
}
