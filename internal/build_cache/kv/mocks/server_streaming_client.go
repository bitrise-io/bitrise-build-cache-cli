package mocks

import (
	"context"

	"google.golang.org/grpc/metadata"
)

type RecvResult[Res any] struct {
	Response *Res
	Error    error
}

type ServerStreamingClientMock[Res any] struct {
	recvCallCount int
	recvResults   []RecvResult[Res]
}

func NewServerStreamingClientMock[Res any](recvResults []RecvResult[Res]) *ServerStreamingClientMock[Res] {
	return &ServerStreamingClientMock[Res]{
		recvResults: recvResults,
	}
}

func (s *ServerStreamingClientMock[Res]) Recv() (*Res, error) {
	result := s.recvResults[s.recvCallCount]
	s.recvCallCount++

	return result.Response, result.Error
}

func (s *ServerStreamingClientMock[Res]) Header() (metadata.MD, error) {
	//TODO implement me
	panic("implement me")
}

func (s *ServerStreamingClientMock[Res]) Trailer() metadata.MD {
	//TODO implement me
	panic("implement me")
}

func (s *ServerStreamingClientMock[Res]) CloseSend() error {
	//TODO implement me
	panic("implement me")
}

func (s *ServerStreamingClientMock[Res]) Context() context.Context {
	//TODO implement me
	panic("implement me")
}

func (s *ServerStreamingClientMock[Res]) SendMsg(m any) error {
	//TODO implement me
	panic("implement me")
}

func (s *ServerStreamingClientMock[Res]) RecvMsg(m any) error {
	//TODO implement me
	panic("implement me")
}
