// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        (unknown)
// source: kv_storage/kv_storage.proto

package kv_storage

import (
	bytestream "google.golang.org/genproto/googleapis/bytestream"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type DeleteResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Ok uint32 `protobuf:"varint,1,opt,name=ok,proto3" json:"ok,omitempty"`
}

func (x *DeleteResponse) Reset() {
	*x = DeleteResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_kv_storage_kv_storage_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *DeleteResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeleteResponse) ProtoMessage() {}

func (x *DeleteResponse) ProtoReflect() protoreflect.Message {
	mi := &file_kv_storage_kv_storage_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use DeleteResponse.ProtoReflect.Descriptor instead.
func (*DeleteResponse) Descriptor() ([]byte, []int) {
	return file_kv_storage_kv_storage_proto_rawDescGZIP(), []int{0}
}

func (x *DeleteResponse) GetOk() uint32 {
	if x != nil {
		return x.Ok
	}
	return 0
}

var File_kv_storage_kv_storage_proto protoreflect.FileDescriptor

var file_kv_storage_kv_storage_proto_rawDesc = []byte{
	0x0a, 0x1b, 0x6b, 0x76, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x2f, 0x6b, 0x76, 0x5f,
	0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x0a, 0x6b,
	0x76, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x1a, 0x22, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2f, 0x62, 0x79, 0x74, 0x65, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x2f, 0x62, 0x79, 0x74,
	0x65, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x20, 0x0a,
	0x0e, 0x44, 0x65, 0x6c, 0x65, 0x74, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12,
	0x0e, 0x0a, 0x02, 0x6f, 0x6b, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x02, 0x6f, 0x6b, 0x32,
	0xe7, 0x01, 0x0a, 0x09, 0x4b, 0x56, 0x53, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x12, 0x48, 0x0a,
	0x03, 0x47, 0x65, 0x74, 0x12, 0x1e, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x62, 0x79,
	0x74, 0x65, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x2e, 0x52, 0x65, 0x61, 0x64, 0x52, 0x65, 0x71,
	0x75, 0x65, 0x73, 0x74, 0x1a, 0x1f, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x62, 0x79,
	0x74, 0x65, 0x73, 0x74, 0x72, 0x65, 0x61, 0x6d, 0x2e, 0x52, 0x65, 0x61, 0x64, 0x52, 0x65, 0x73,
	0x70, 0x6f, 0x6e, 0x73, 0x65, 0x30, 0x01, 0x12, 0x4a, 0x0a, 0x03, 0x50, 0x75, 0x74, 0x12, 0x1f,
	0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x62, 0x79, 0x74, 0x65, 0x73, 0x74, 0x72, 0x65,
	0x61, 0x6d, 0x2e, 0x57, 0x72, 0x69, 0x74, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a,
	0x20, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x62, 0x79, 0x74, 0x65, 0x73, 0x74, 0x72,
	0x65, 0x61, 0x6d, 0x2e, 0x57, 0x72, 0x69, 0x74, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73,
	0x65, 0x28, 0x01, 0x12, 0x44, 0x0a, 0x06, 0x44, 0x65, 0x6c, 0x65, 0x74, 0x65, 0x12, 0x1e, 0x2e,
	0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x62, 0x79, 0x74, 0x65, 0x73, 0x74, 0x72, 0x65, 0x61,
	0x6d, 0x2e, 0x52, 0x65, 0x61, 0x64, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1a, 0x2e,
	0x6b, 0x76, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x2e, 0x44, 0x65, 0x6c, 0x65, 0x74,
	0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x42, 0x31, 0x5a, 0x2f, 0x67, 0x69, 0x74,
	0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x66, 0x6c, 0x61, 0x72, 0x65, 0x62, 0x75, 0x69,
	0x6c, 0x64, 0x2f, 0x66, 0x6c, 0x61, 0x72, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75,
	0x66, 0x2f, 0x6b, 0x76, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x62, 0x06, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_kv_storage_kv_storage_proto_rawDescOnce sync.Once
	file_kv_storage_kv_storage_proto_rawDescData = file_kv_storage_kv_storage_proto_rawDesc
)

func file_kv_storage_kv_storage_proto_rawDescGZIP() []byte {
	file_kv_storage_kv_storage_proto_rawDescOnce.Do(func() {
		file_kv_storage_kv_storage_proto_rawDescData = protoimpl.X.CompressGZIP(file_kv_storage_kv_storage_proto_rawDescData)
	})
	return file_kv_storage_kv_storage_proto_rawDescData
}

var file_kv_storage_kv_storage_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_kv_storage_kv_storage_proto_goTypes = []interface{}{
	(*DeleteResponse)(nil),           // 0: kv_storage.DeleteResponse
	(*bytestream.ReadRequest)(nil),   // 1: google.bytestream.ReadRequest
	(*bytestream.WriteRequest)(nil),  // 2: google.bytestream.WriteRequest
	(*bytestream.ReadResponse)(nil),  // 3: google.bytestream.ReadResponse
	(*bytestream.WriteResponse)(nil), // 4: google.bytestream.WriteResponse
}
var file_kv_storage_kv_storage_proto_depIdxs = []int32{
	1, // 0: kv_storage.KVStorage.Get:input_type -> google.bytestream.ReadRequest
	2, // 1: kv_storage.KVStorage.Put:input_type -> google.bytestream.WriteRequest
	1, // 2: kv_storage.KVStorage.Delete:input_type -> google.bytestream.ReadRequest
	3, // 3: kv_storage.KVStorage.Get:output_type -> google.bytestream.ReadResponse
	4, // 4: kv_storage.KVStorage.Put:output_type -> google.bytestream.WriteResponse
	0, // 5: kv_storage.KVStorage.Delete:output_type -> kv_storage.DeleteResponse
	3, // [3:6] is the sub-list for method output_type
	0, // [0:3] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_kv_storage_kv_storage_proto_init() }
func file_kv_storage_kv_storage_proto_init() {
	if File_kv_storage_kv_storage_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_kv_storage_kv_storage_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*DeleteResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_kv_storage_kv_storage_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_kv_storage_kv_storage_proto_goTypes,
		DependencyIndexes: file_kv_storage_kv_storage_proto_depIdxs,
		MessageInfos:      file_kv_storage_kv_storage_proto_msgTypes,
	}.Build()
	File_kv_storage_kv_storage_proto = out.File
	file_kv_storage_kv_storage_proto_rawDesc = nil
	file_kv_storage_kv_storage_proto_goTypes = nil
	file_kv_storage_kv_storage_proto_depIdxs = nil
}