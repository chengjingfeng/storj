// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: certificate.proto

package pb

import proto "github.com/gogo/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "github.com/gogo/protobuf/gogoproto"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion2 // please upgrade the proto package

type SigningRequest struct {
	AuthToken            string   `protobuf:"bytes,1,opt,name=auth_token,json=authToken,proto3" json:"auth_token,omitempty"`
	Timestamp            int64    `protobuf:"varint,2,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SigningRequest) Reset()         { *m = SigningRequest{} }
func (m *SigningRequest) String() string { return proto.CompactTextString(m) }
func (*SigningRequest) ProtoMessage()    {}
func (*SigningRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_certificate_80970fe789528feb, []int{0}
}
func (m *SigningRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SigningRequest.Unmarshal(m, b)
}
func (m *SigningRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SigningRequest.Marshal(b, m, deterministic)
}
func (dst *SigningRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SigningRequest.Merge(dst, src)
}
func (m *SigningRequest) XXX_Size() int {
	return xxx_messageInfo_SigningRequest.Size(m)
}
func (m *SigningRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_SigningRequest.DiscardUnknown(m)
}

var xxx_messageInfo_SigningRequest proto.InternalMessageInfo

func (m *SigningRequest) GetAuthToken() string {
	if m != nil {
		return m.AuthToken
	}
	return ""
}

func (m *SigningRequest) GetTimestamp() int64 {
	if m != nil {
		return m.Timestamp
	}
	return 0
}

type SigningResponse struct {
	Cert                 []byte   `protobuf:"bytes,1,opt,name=cert,proto3" json:"cert,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SigningResponse) Reset()         { *m = SigningResponse{} }
func (m *SigningResponse) String() string { return proto.CompactTextString(m) }
func (*SigningResponse) ProtoMessage()    {}
func (*SigningResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_certificate_80970fe789528feb, []int{1}
}
func (m *SigningResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_SigningResponse.Unmarshal(m, b)
}
func (m *SigningResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_SigningResponse.Marshal(b, m, deterministic)
}
func (dst *SigningResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_SigningResponse.Merge(dst, src)
}
func (m *SigningResponse) XXX_Size() int {
	return xxx_messageInfo_SigningResponse.Size(m)
}
func (m *SigningResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_SigningResponse.DiscardUnknown(m)
}

var xxx_messageInfo_SigningResponse proto.InternalMessageInfo

func (m *SigningResponse) GetCert() []byte {
	if m != nil {
		return m.Cert
	}
	return nil
}

func init() {
	proto.RegisterType((*SigningRequest)(nil), "node.SigningRequest")
	proto.RegisterType((*SigningResponse)(nil), "node.SigningResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// CertificatesClient is the client API for Certificates service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type CertificatesClient interface {
	Sign(ctx context.Context, in *SigningRequest, opts ...grpc.CallOption) (*SigningResponse, error)
}

type certificatesClient struct {
	cc *grpc.ClientConn
}

func NewCertificatesClient(cc *grpc.ClientConn) CertificatesClient {
	return &certificatesClient{cc}
}

func (c *certificatesClient) Sign(ctx context.Context, in *SigningRequest, opts ...grpc.CallOption) (*SigningResponse, error) {
	out := new(SigningResponse)
	err := c.cc.Invoke(ctx, "/node.Certificates/Sign", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CertificatesServer is the server API for Certificates service.
type CertificatesServer interface {
	Sign(context.Context, *SigningRequest) (*SigningResponse, error)
}

func RegisterCertificatesServer(s *grpc.Server, srv CertificatesServer) {
	s.RegisterService(&_Certificates_serviceDesc, srv)
}

func _Certificates_Sign_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SigningRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CertificatesServer).Sign(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/node.Certificates/Sign",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CertificatesServer).Sign(ctx, req.(*SigningRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Certificates_serviceDesc = grpc.ServiceDesc{
	ServiceName: "node.Certificates",
	HandlerType: (*CertificatesServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Sign",
			Handler:    _Certificates_Sign_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "certificate.proto",
}

func init() { proto.RegisterFile("certificate.proto", fileDescriptor_certificate_80970fe789528feb) }

var fileDescriptor_certificate_80970fe789528feb = []byte{
	// 188 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x12, 0x4c, 0x4e, 0x2d, 0x2a,
	0xc9, 0x4c, 0xcb, 0x4c, 0x4e, 0x2c, 0x49, 0xd5, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x62, 0xc9,
	0xcb, 0x4f, 0x49, 0x95, 0xe2, 0x4a, 0xcf, 0x4f, 0xcf, 0x87, 0x88, 0x28, 0xf9, 0x72, 0xf1, 0x05,
	0x67, 0xa6, 0xe7, 0x65, 0xe6, 0xa5, 0x07, 0xa5, 0x16, 0x96, 0xa6, 0x16, 0x97, 0x08, 0xc9, 0x72,
	0x71, 0x25, 0x96, 0x96, 0x64, 0xc4, 0x97, 0xe4, 0x67, 0xa7, 0xe6, 0x49, 0x30, 0x2a, 0x30, 0x6a,
	0x70, 0x06, 0x71, 0x82, 0x44, 0x42, 0x40, 0x02, 0x42, 0x32, 0x5c, 0x9c, 0x25, 0x99, 0xb9, 0xa9,
	0xc5, 0x25, 0x89, 0xb9, 0x05, 0x12, 0x4c, 0x0a, 0x8c, 0x1a, 0xcc, 0x41, 0x08, 0x01, 0x25, 0x55,
	0x2e, 0x7e, 0xb8, 0x71, 0xc5, 0x05, 0xf9, 0x79, 0xc5, 0xa9, 0x42, 0x42, 0x5c, 0x2c, 0x20, 0x87,
	0x80, 0x4d, 0xe2, 0x09, 0x02, 0xb3, 0x8d, 0x9c, 0xb9, 0x78, 0x9c, 0x11, 0x8e, 0x2b, 0x16, 0x32,
	0xe6, 0x62, 0x01, 0x69, 0x13, 0x12, 0xd1, 0x03, 0x39, 0x50, 0x0f, 0xd5, 0x45, 0x52, 0xa2, 0x68,
	0xa2, 0x10, 0x83, 0x9d, 0x58, 0xa2, 0x98, 0x0a, 0x92, 0x92, 0xd8, 0xc0, 0xfe, 0x30, 0x06, 0x04,
	0x00, 0x00, 0xff, 0xff, 0x4b, 0x1b, 0x87, 0x66, 0xee, 0x00, 0x00, 0x00,
}