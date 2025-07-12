package control

import (
	"context"

	"github.com/core-tools/hsu-master/pkg/domain"
	"github.com/core-tools/hsu-master/pkg/generated/api/proto"
	"github.com/core-tools/hsu-master/pkg/logging"

	"google.golang.org/grpc"
)

func RegisterGRPCServerHandler(grpcServerRegistrar grpc.ServiceRegistrar, handler domain.Contract, logger logging.Logger) {
	proto.RegisterMasterServiceServer(grpcServerRegistrar, &grpcServerHandler{
		handler: handler,
		logger:  logger,
	})
}

type grpcServerHandler struct {
	proto.UnimplementedMasterServiceServer
	handler domain.Contract
	logger  logging.Logger
}

func (h *grpcServerHandler) Status(ctx context.Context, statusRequest *proto.StatusRequest) (*proto.StatusResponse, error) {
	status, err := h.handler.Status(ctx)
	if err != nil {
		h.logger.Errorf("Status server handler: %v", err)
		return nil, err
	}
	h.logger.Debugf("Status server handler done")
	return &proto.StatusResponse{Status: status}, nil
}
