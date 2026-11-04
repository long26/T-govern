//// Package compression implements block data compression functionality
package compression

//
//import (
//	"bytes"
//	"compress/gzip"
//	"io"
//)
//
//// 区块压缩 删了
//
//// CompressBlock compresses the given block data using gzip compression
//func CompressBlock(data []byte) ([]byte, error) {
//	var b bytes.Buffer
//	w := gzip.NewWriter(&b)
//
//	_, err := w.Write(data)
//	if err != nil {
//		return nil, err
//	}
//
//	err = w.Close()
//	if err != nil {
//		return nil, err
//	}
//
//	return b.Bytes(), nil
//}
//
//// DecompressBlock decompresses the given compressed block data
//func DecompressBlock(compressedData []byte) ([]byte, error) {
//	b := bytes.NewBuffer(compressedData)
//	r, err := gzip.NewReader(b)
//	if err != nil {
//		return nil, err
//	}
//	defer r.Close()
//
//	var decompressed bytes.Buffer
//	_, err = io.Copy(&decompressed, r)
//	if err != nil {
//		return nil, err
//	}
//
//	return decompressed.Bytes(), nil
//}
