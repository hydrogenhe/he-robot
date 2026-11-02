package bilibili

import (
	"errors"
	"google.golang.org/protobuf/encoding/protowire"
)

// broadcastFields 按标准Protobuf线格式取字段，保留uint64精度并限制长度，不解析消息正文。
func broadcastFields(data []byte, number protowire.Number) ([][]byte, []uint64, error) {
	bytes := [][]byte{}
	values := []uint64{}
	if len(data) > 512*1024 {
		return nil, nil, errors.New("broadcast frame too large")
	}
	for len(data) > 0 {
		field, kind, n := protowire.ConsumeTag(data)
		if n < 0 || field < 1 {
			return nil, nil, errors.New("invalid broadcast tag")
		}
		data = data[n:]
		switch kind {
		case protowire.BytesType:
			value, size := protowire.ConsumeBytes(data)
			if size < 0 {
				return nil, nil, errors.New("invalid broadcast bytes")
			}
			if field == number {
				bytes = append(bytes, value)
			}
			data = data[size:]
		case protowire.VarintType:
			value, size := protowire.ConsumeVarint(data)
			if size < 0 {
				return nil, nil, errors.New("invalid broadcast integer")
			}
			if field == number {
				values = append(values, value)
			}
			data = data[size:]
		default:
			size := protowire.ConsumeFieldValue(field, kind, data)
			if size < 0 {
				return nil, nil, errors.New("invalid broadcast field")
			}
			data = data[size:]
		}
	}
	return bytes, values, nil
}
func broadcastBytes(data []byte, number protowire.Number) []byte {
	b, _, _ := broadcastFields(data, number)
	if len(b) > 0 {
		return b[0]
	}
	return nil
}
func broadcastInt(data []byte, number protowire.Number) uint64 {
	_, v, _ := broadcastFields(data, number)
	if len(v) > 0 {
		return v[0]
	}
	return 0
}
func broadcastPut(data []byte, number protowire.Number, value []byte) []byte {
	data = protowire.AppendTag(data, number, protowire.BytesType)
	return protowire.AppendBytes(data, value)
}
func broadcastPacket(sequence uint64, path, typeName string, body []byte) []byte {
	options := protowire.AppendTag(nil, 2, protowire.VarintType)
	options = protowire.AppendVarint(options, sequence)
	any := broadcastPut(nil, 1, []byte("type.googleapis.com/"+typeName))
	any = broadcastPut(any, 2, body)
	frame := broadcastPut(nil, 1, options)
	frame = broadcastPut(frame, 2, []byte(path))
	return broadcastPut(frame, 3, any)
}
