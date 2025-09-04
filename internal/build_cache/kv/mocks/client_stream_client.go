package mocks

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var _ grpc.ClientStream = (*ClientStreamClientMock[any, any])(nil)

type ClientStreamClientMock[Req any, Res any] struct {
	response      *Res
	requests      []*Req
	sendCallCount int
	sendErrors    []error
}

func NewClientStreamClientMock[Req any, Res any](
	response *Res,
	sendErrors []error,
) *ClientStreamClientMock[Req, Res] {
	return &ClientStreamClientMock[Req, Res]{
		response:   response,
		sendErrors: sendErrors,
	}
}

func (c *ClientStreamClientMock[Req, Res]) Requests() []*Req {
	return c.requests
}

func (c *ClientStreamClientMock[Req, Res]) Send(req *Req) error {
	c.requests = append(c.requests, req)

	err := c.sendErrors[c.sendCallCount]
	c.sendCallCount++

	return err
}

func (c *ClientStreamClientMock[Req, Res]) CloseAndRecv() (*Res, error) {
	return c.response, nil
}

func (c *ClientStreamClientMock[Req, Res]) Header() (metadata.MD, error) {
	//TODO implement me
	panic("implement me")
}

func (c *ClientStreamClientMock[Req, Res]) Trailer() metadata.MD {
	//TODO implement me
	panic("implement me")
}

func (c *ClientStreamClientMock[Req, Res]) CloseSend() error {
	//TODO implement me
	panic("implement me")
}

func (c *ClientStreamClientMock[Req, Res]) Context() context.Context {
	//TODO implement me
	panic("implement me")
}

func (c *ClientStreamClientMock[Req, Res]) SendMsg(m any) error {
	//TODO implement me
	panic("implement me")
}

func (c *ClientStreamClientMock[Req, Res]) RecvMsg(m any) error {
	//TODO implement me
	panic("implement me")
}
